package evaluator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

var reIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

func isIdentifier(s string) bool {
	return reIdent.MatchString(s)
}

// normalizeTraversal normalizes an index traversal to an attribute traversal for known cases.
// (i.e. x["foo"] is effectively turned to x.foo).
func normalizeTraversal(t hcl.Traversal) hcl.Traversal {
	var ret hcl.Traversal
loop:
	for _, item := range t {
		switch item := item.(type) {
		case hcl.TraverseRoot:
			ret = append(ret, item)
		case hcl.TraverseAttr:
			ret = append(ret, item)
		case hcl.TraverseIndex:
			k := item.Key
			if k.Type() == cty.String && isIdentifier(k.AsString()) {
				ret = append(ret, hcl.TraverseAttr{
					Name:     k.AsString(),
					SrcRange: item.SrcRange,
				})
				continue loop
			}
			ret = append(ret, item)
		default:
			panic(fmt.Errorf("unexpected traversal type: %T", item))
		}
	}
	return ret
}

// hasVariable returns true if the supplied name is defined in the current
// or any ancestor context.
func hasVariable(ctx *hcl.EvalContext, name string) bool {
	c := ctx
	for c != nil {
		if _, ok := c.Variables[name]; ok {
			return true
		}
		c = c.Parent()
	}
	return false
}

// makeTables returns a map of top-level keys to their corresponding objects.
func makeTables(ctx *hcl.EvalContext) map[string]DynamicObject {
	return map[string]DynamicObject{
		reservedReq:  extractSymbolTable(ctx, reservedReq),
		reservedSelf: extractSymbolTable(ctx, reservedSelf),
		reservedArg:  extractSymbolTable(ctx, reservedArg),
	}
}

// extractSymbolTable returns a map of values keyed by symbols under a specific namespace (e.g. `self` or `req`)
// It expects the top level entry to be an object. It will panic if this is not the case.
func extractSymbolTable(ctx *hcl.EvalContext, namespace string) DynamicObject {
	for ctx != nil {
		symbols, ok := ctx.Variables[namespace]
		if ok {
			return symbols.AsValueMap()
		}
		ctx = ctx.Parent()
	}
	return DynamicObject{}
}

// createSelfChildContext creates a `self` var in the supplied context using the `self` var defined
// in the nearest parent context and augmenting it with the additional values passed.
func createSelfChildContext(ctx *hcl.EvalContext, vars DynamicObject) *hcl.EvalContext {
	table := extractSymbolTable(ctx, reservedSelf)
	for k, v := range vars {
		table[k] = v
	}
	child := ctx.NewChild()
	child.Variables = DynamicObject{
		reservedSelf: cty.ObjectVal(table),
	}
	return child
}

// valueToInterface returns the supplied dynamic value as a Go type.
func valueToInterface(val cty.Value) (any, error) {
	jsonBytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, err
	}
	var result any
	if err = json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func valueToStruct(val cty.Value) (*structpb.Struct, error) {
	jsonBytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, err
	}
	var result structpb.Struct
	if err := protojson.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func valueToStructWithAnnotations(val cty.Value, a map[string]string) (*structpb.Struct, error) {
	if len(a) == 0 {
		return valueToStruct(val)
	}

	jsonBytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, errors.Wrap(err, "marshal cty to json")
	}

	var result map[string]any
	if err = json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, errors.Wrap(err, "unmarshal cty to json")
	}

	meta, ok := result["metadata"]
	if !ok {
		meta = map[string]any{}
		result["metadata"] = meta
	}
	metaObj, ok := meta.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected metadata to be a map[string]any, got %T", meta)
	}

	annotations, ok := metaObj["annotations"]
	if !ok {
		annotations = map[string]any{}
		metaObj["annotations"] = annotations
	}
	annotationsObj, ok := annotations.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected annotations to be a map[string]any, got %T", meta)
	}

	for k, v := range a {
		annotationsObj[k] = v
	}
	ret, err := structpb.NewStruct(result)
	if err != nil {
		return nil, errors.Wrapf(err, "convert result to struct")
	}
	return ret, nil
}

type iteration struct {
	key   cty.Value
	value cty.Value
}

func extractIterations(forEachValue cty.Value) ([]iteration, error) {
	if forEachValue.IsNull() || !forEachValue.IsWhollyKnown() {
		return nil, fmt.Errorf("for_each value is null or unknown")
	}
	var ret []iteration
	switch {
	case forEachValue.Type().IsListType() || forEachValue.Type().IsTupleType():
		elements := forEachValue.AsValueSlice()
		for i, element := range elements {
			key := cty.NumberIntVal(int64(i))
			ret = append(ret, iteration{key: key, value: element})
		}
	case forEachValue.Type().IsMapType() || forEachValue.Type().IsObjectType():
		elements := forEachValue.AsValueMap()
		for keyStr, value := range elements {
			key := cty.StringVal(keyStr)
			ret = append(ret, iteration{key: key, value: value})
		}
	case forEachValue.Type().IsSetType():
		// convert set to list first, then iterate
		// for sets, both key and value are set to the value similar to how Terraform does it.
		elements := forEachValue.AsValueSlice()
		for _, element := range elements {
			ret = append(ret, iteration{key: element, value: element})
		}
	default:
		return nil, fmt.Errorf("for_each value is not iterable, found type %v", forEachValue.Type().FriendlyName())
	}
	return ret, nil
}

func unify(inputs ...Object) (Object, error) {
	var unifyObjects func(path string, objects ...Object) (Object, error)
	unifyObjects = func(path string, objects ...Object) (Object, error) {
		ret := Object{}
		for _, obj := range objects {
			for k, v := range obj {
				currentPath := k
				if path != "" {
					currentPath = fmt.Sprintf("%s.%s", path, k)
				}
				existing, ok := ret[k]
				if !ok {
					ret[k] = v
					continue
				}
				existingType := reflect.TypeOf(existing)
				inputType := reflect.TypeOf(v)

				if existingType != inputType {
					return nil, fmt.Errorf("type mismatch for key %s:  %s v/s %s", currentPath, inputType, existingType)
				}

				if e, ok := existing.(Object); ok {
					//nolint: forcetypeassert
					unified, err := unifyObjects(currentPath, v.(Object), e)
					if err != nil {
						return nil, err
					}
					ret[k] = unified
					continue
				}

				if !reflect.DeepEqual(v, existing) {
					return nil, fmt.Errorf("values for key %s not equal", currentPath)
				}
			}
		}
		return ret, nil
	}
	return unifyObjects("", inputs...)
}

func unifyBytes(inputs ...map[string][]byte) (map[string][]byte, error) {
	ret := map[string][]byte{}
	for _, input := range inputs {
		for k, v := range input {
			existing, ok := ret[k]
			if !ok {
				ret[k] = v
				continue
			}
			if !bytes.Equal(v, existing) {
				return nil, fmt.Errorf("values for key %s not equal", k)
			}
		}
	}
	return ret, nil
}

// findUnknownPaths walks the value and returns a list of paths to unknown values.
func findUnknownPaths(val cty.Value) ([]string, error) {
	var unknownPaths []string
	if err := cty.Walk(val, func(path cty.Path, v cty.Value) (bool, error) {
		if !v.IsKnown() {
			unknownPaths = append(unknownPaths, path2string(path))
			return true, nil
		}
		return true, nil
	}); err != nil {
		return unknownPaths, err
	}

	return unknownPaths, nil
}

// unknownSegmentMarker is used to represent segments we don't support decoding.
const unknownSegmentMarker = "<?>"

// path2string converts a cty.Path to a human-readable string.
func path2string(path cty.Path) string {
	segments := make([]string, 0, len(path))

	for _, p := range path {
		switch s := p.(type) {
		case cty.GetAttrStep:
			segments = append(segments, fmt.Sprintf(".%s", s.Name))
		case cty.IndexStep:
			switch s.Key.Type() {
			case cty.String:
				segments = append(segments, fmt.Sprintf("[%s]", s.Key.AsString()))
			case cty.Number:
				segments = append(segments, fmt.Sprintf("[%s]", s.Key.AsBigFloat().Text('f', 0)))
			default:
				segments = append(segments, unknownSegmentMarker)
			}
		default:
			segments = append(segments, unknownSegmentMarker)
		}
	}
	return strings.Join(segments, "")
}

// err2diag converts an error to a hcl.Diagnostic.
func err2diag(err error) *hcl.Diagnostic {
	return &hcl.Diagnostic{
		Severity: hcl.DiagError,
		Summary:  err.Error(),
	}
}

// mapDiagnosticSeverity maps the severity of the diagnostics from src to dst.
//
// This is a destructive operation, clone the diags before calling this function if you need the original.
// nolint:unparam
func mapDiagnosticSeverity(diags hcl.Diagnostics, src, dst hcl.DiagnosticSeverity) hcl.Diagnostics {
	for i := range diags {
		if diags[i].Severity == src {
			diags[i].Severity = dst
		}
	}
	return diags
}

// ptr returns a pointer to the supplied value.
func ptr[T any](v T) *T {
	return &v
}

// sortDiagsBySeverity sorts the supplied diagnostics by severity.
func sortDiagsBySeverity(diags hcl.Diagnostics) hcl.Diagnostics {
	sort.SliceStable(diags, func(i, j int) bool {
		return diags[i].Severity < diags[j].Severity
	})
	return diags
}
