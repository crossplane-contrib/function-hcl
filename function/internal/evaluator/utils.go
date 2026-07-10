package evaluator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	"google.golang.org/protobuf/types/known/structpb"
)

// hasVariable returns true if the supplied name is defined in the current or any ancestor context.
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

func removeNulls(val Object) {
	for k, v := range val {
		if v == nil {
			delete(val, k)
			continue
		}
		switch v := v.(type) {
		case Object:
			removeNulls(v)
		case []any:
			for _, item := range v {
				if obj, ok := item.(Object); ok {
					removeNulls(obj)
				}
			}
		}
	}
}

// valueToStructWithAnnotations returns the supplied dynamic value as a protobuf struct after
// injecting the supplied annotations into it.
func valueToStructWithAnnotations(val cty.Value, a map[string]string) (*structpb.Struct, error) {
	jsonBytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, errors.Wrap(err, "marshal cty to json")
	}

	var result map[string]any
	if err = json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, errors.Wrap(err, "unmarshal cty to json")
	}

	// remove all null keys from the result
	removeNulls(result)

	if len(a) > 0 {
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
	}
	ret, err := structpb.NewStruct(result)
	if err != nil {
		return nil, errors.Wrapf(err, "convert result to struct")
	}
	return ret, nil
}

// iteration stores the key and value for a specific for_each iteration.
type iteration struct {
	key   cty.Value
	value cty.Value
}

// extractIterations returns a list of iterations for the supplied value which must be a collection of some sort.
// For sets, both key and value are set to the set element.
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

// unify unifies the supplied objects by merging values ensuring that leaf-level values are identical in the event
// that multiple objects contain the same leaf key.
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

// unifyBytes unifies the supplied maps with the same semantics as unify.
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
			unknownPaths = append(unknownPaths, path2String(path))
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

// path2String converts a cty.Path to a human-readable string.
func path2String(path cty.Path) string {
	segments := make([]string, 0, len(path))

	for _, p := range path {
		switch s := p.(type) {
		case cty.GetAttrStep:
			segments = append(segments, fmt.Sprintf(".%s", s.Name))
		case cty.IndexStep:
			if !s.Key.IsKnown() {
				segments = append(segments, unknownSegmentMarker)
				continue
			}
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
