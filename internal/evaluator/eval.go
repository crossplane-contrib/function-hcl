package evaluator

import (
	"fmt"
	"strings"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/functions"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/hclutils"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	maxDiscardsToDisplay = 3
)

func (e *Evaluator) doEval(in *fnv1.RunFunctionRequest, files ...File) (_ *fnv1.RunFunctionResponse, finalErr error) {
	// note: when returning something using diags from this function, we sort by severity first
	// this is in order to have at least one error show up in formatted errors.
	defer func() {
		if finalErr != nil {
			diags, ok := finalErr.(hcl.Diagnostics)
			if ok {
				finalErr = sortDiagsBySeverity(diags)
			}
		}
	}()

	// parse all files
	mergedBody, diags := e.toContent(files)
	if diags.HasErrors() {
		return nil, diags
	}

	ctx, ds := e.processFunctions(mergedBody)
	diags = diags.Extend(ds)
	if diags.HasErrors() {
		return nil, diags
	}

	// make vars in cty format and set up the initial eval context
	ctx, err := e.makeVars(ctx, in)
	if err != nil {
		return nil, diags.Append(hclutils.Err2Diag(err))
	}

	// process top-level blocks as a group
	ds = e.processGroup(ctx, mergedBody)
	diags = diags.Extend(ds)
	if ds.HasErrors() {
		return nil, diags
	}

	// create the response from internal state.
	res, err := e.toResponse(diags)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// processFunctions processes all function blocks at the top-level and returns an evaluation
// context that includes all supported functions with an `invoke` function in addition.
func (e *Evaluator) processFunctions(content *hcl.BodyContent) (*hcl.EvalContext, hcl.Diagnostics) {
	p := functions.NewProcessor()
	diags := p.Process(content)
	if diags.HasErrors() {
		return nil, diags
	}
	return p.RootContext(nil), nil
}

func (e *Evaluator) toBodies(files []File) ([]hcl.Body, hcl.Diagnostics) {
	parser := hclparse.NewParser()
	var bodies []hcl.Body
	for _, file := range files {
		hclFile, diags := parser.ParseHCL([]byte(file.Content), file.Name)
		if diags.HasErrors() {
			return nil, diags
		}
		e.files[file.Name] = hclFile
		b, ok := hclFile.Body.(*hclsyntax.Body)
		if !ok {
			panic(fmt.Errorf("internal error: unable to convert HCL body to desired type"))
		}
		bodies = append(bodies, b)
	}
	return bodies, nil
}

func (e *Evaluator) makeContent(bodies []hcl.Body) (*hcl.BodyContent, hcl.Diagnostics) {
	var d hcl.Diagnostics
	ret := &hcl.BodyContent{}
	for _, body := range bodies {
		content, diags := body.Content(topLevelSchema())
		d = d.Extend(diags)
		if content != nil {
			ret.Blocks = append(ret.Blocks, content.Blocks...)
		}
	}
	if d.HasErrors() {
		return nil, d
	}
	return ret, nil
}

func (e *Evaluator) toContent(files []File) (*hcl.BodyContent, hcl.Diagnostics) {
	bodies, diags := e.toBodies(files)
	if diags.HasErrors() {
		return nil, diags
	}
	return e.makeContent(bodies)
}

// evaluateCondition looks for an optional condition attribute in the supplied content and return false if the content
// is to be skipped.
func (e *Evaluator) evaluateCondition(ctx *hcl.EvalContext, content *hcl.BodyContent, et DiscardType, name string) (bool, hcl.Diagnostics) {
	if condAttr, exists := content.Attributes[attrCondition]; exists {
		val, diags := condAttr.Expr.Value(ctx)
		if diags.HasErrors() {
			return false, diags
		}
		if val.Type() != cty.Bool {
			return false, diags.Append(hclutils.Err2Diag(fmt.Errorf("got type %s, expected %s", val.Type(), cty.Bool)))
		}
		if !val.True() {
			e.discard(DiscardItem{
				Type:        et,
				Reason:      discardReasonUserCondition,
				Name:        name,
				SourceRange: condAttr.Range.String(),
			})
		}
		return val.True(), diags
	}
	return true, nil
}

// toResponse creates a RunFunctionResponse from internal state.
func (e *Evaluator) toResponse(diags hcl.Diagnostics) (*fnv1.RunFunctionResponse, error) {
	ret := fnv1.RunFunctionResponse{}

	if ret.Desired == nil {
		ret.Desired = &fnv1.State{}
	}
	if ret.Desired.Resources == nil {
		ret.Desired.Resources = map[string]*fnv1.Resource{}
	}
	for name, res := range e.desiredResources {
		ret.Desired.Resources[name] = &fnv1.Resource{Resource: res}
	}

	ensureDesiredComposite := func() {
		if ret.Desired.Composite == nil {
			ret.Desired.Composite = &fnv1.Resource{}
		}
	}

	if len(e.compositeStatuses) > 0 {
		st, err := unify(e.compositeStatuses...)
		if err != nil {
			return nil, errors.Wrap(err, "unify composite status")
		}
		obj := Object{
			"status": st,
		}
		s, err := structpb.NewStruct(obj)
		if err != nil {
			return nil, fmt.Errorf("unexpected error converting composite status: %v", err)
		}
		ensureDesiredComposite()
		ret.Desired.Composite.Resource = s
	}

	if len(e.compositeConnections) > 0 {
		ensureDesiredComposite()
		u, err := unifyBytes(e.compositeConnections...)
		if err != nil {
			return nil, errors.Wrap(err, "unify composite connection")
		}
		ret.Desired.Composite.ConnectionDetails = u
	}

	if len(e.contexts) > 0 {
		ctx, err := unify(e.contexts...)
		if err != nil {
			return nil, errors.Wrap(err, "unify context")
		}
		s, err := structpb.NewStruct(ctx)
		if err != nil {
			return nil, fmt.Errorf("unexpected error converting context: %v", err)
		}
		ret.Context = s
	}

	if len(e.requirements) > 0 {
		ret.Requirements = &fnv1.Requirements{
			ExtraResources: e.requirements,
		}
	}

	for name, val := range e.ready {
		desired := ret.Desired.Resources[name]
		if desired == nil {
			panic(fmt.Sprintf("internal error: no desired resource found for %s when readiness set", name))
		}
		desired.Ready = fnv1.Ready(val)
	}

	tg := fnv1.Target_TARGET_COMPOSITE
	var discarded []string
	msg := ""
	for _, di := range e.discards {
		if di.Reason == discardReasonUserCondition {
			continue
		}
		resultReason := string(di.Reason)
		r := &fnv1.Result{
			Severity: fnv1.Severity_SEVERITY_WARNING,
			Message:  di.MessageString(),
			Target:   &tg,
			Reason:   &resultReason,
		}
		ret.Results = append(ret.Results, r)
		if len(discarded) < maxDiscardsToDisplay {
			discarded = append(discarded, fmt.Sprintf("%s %s", di.Type, di.Name))
		}
	}

	if len(discarded) > 0 {
		msg = strings.Join(discarded, ", ")
		if len(ret.Results) > maxDiscardsToDisplay {
			msg += fmt.Sprintf(" and %d more items incomplete", len(ret.Results)-maxDiscardsToDisplay)
		} else {
			msg += " incomplete"
		}
	} else {
		msg = "all items complete"
	}
	c := fnv1.Status_STATUS_CONDITION_TRUE
	resultReason := "AllItemsProcessed"
	if len(ret.Results) > 0 {
		resultReason = "IncompleteItemsPresent"
		c = fnv1.Status_STATUS_CONDITION_FALSE
	}

	cond := fnv1.Condition{
		Type:    "FullyResolved",
		Target:  &tg,
		Status:  c,
		Reason:  resultReason,
		Message: &msg,
	}
	ret.Conditions = append(ret.Conditions, &cond)

	// Add diagnostics info
	e.addDiagnosticsInfo(&ret, diags)

	return &ret, nil
}

// addDiagnosticsInfo adds diagnostics information to the response.
func (e *Evaluator) addDiagnosticsInfo(ret *fnv1.RunFunctionResponse, diags hcl.Diagnostics) {
	target := ptr(fnv1.Target_TARGET_COMPOSITE)
	resultReason := ptr("HclDiagnostics")
	condition := &fnv1.Condition{
		Type:   "HclDiagnostics",
		Target: target,
		Status: fnv1.Status_STATUS_CONDITION_TRUE,
		Reason: "Eval",
	}

	summaries := make([]string, 0, len(diags))
	for _, diag := range diags {
		if diag.Severity == hcl.DiagWarning {
			summaries = append(summaries, fmt.Sprintf("%s: %s", diag.Subject, diag.Summary))
			condition.Status = fnv1.Status_STATUS_CONDITION_FALSE
		}
	}

	if len(summaries) > 0 {
		r := &fnv1.Result{
			Severity: fnv1.Severity_SEVERITY_WARNING,
			Message:  fmt.Sprintf("warnings: [%s]", strings.Join(summaries, "; ")),
			Target:   target,
			Reason:   resultReason,
		}
		ret.Results = append(ret.Results, r)
		condition.Message = ptr(fmt.Sprintf("hcl.Diagnostics contains %d warnings; %s", len(summaries), strings.Join(summaries, "; ")))
	} else {
		r := &fnv1.Result{
			Severity: fnv1.Severity_SEVERITY_NORMAL,
			Message:  "no warnings",
			Target:   target,
			Reason:   resultReason,
		}
		ret.Results = append(ret.Results, r)
		condition.Message = ptr("hcl.Diagnostics contains no warnings")
	}

	ret.Conditions = append(ret.Conditions, condition)
}

// discard adds a discard item to the evaluator's list.
func (e *Evaluator) discard(el DiscardItem) {
	e.discards = append(e.discards, el)
}

// getObservedResource returns the resource body of the observed
// resource with the supplied name or any empty object.
func (e *Evaluator) getObservedResource(name string) cty.Value {
	return e.existingResourceMap[name]
}

// getObservedConnection returns the connection details of the observed
// resource with the supplied name or any empty object.
func (e *Evaluator) getObservedConnection(name string) cty.Value {
	return e.existingConnectionMap[name]
}

// getObservedCollectionResources returns a list of resources under the
// resource collection with the supplied name, or an empty list.
func (e *Evaluator) getObservedCollectionResources(baseName string) cty.Value {
	return e.collectionResourcesMap[baseName]
}

// getObservedCollectionConnections returns a list of connection details under the
// resource collection with the supplied name, or an empty list.
func (e *Evaluator) getObservedCollectionConnections(baseName string) cty.Value {
	return e.collectionConnectionsMap[baseName]
}

// sourceCode returns the source code associated with the supplied range
// with best-effort processing. Do not rely on this for anything other than
// error messages.
func (e *Evaluator) sourceCode(r hcl.Range) string {
	ret := "[unknown source]"
	f := e.files[r.Filename]
	if f == nil {
		return ret
	}
	if r.End.Byte > len(f.Bytes) {
		return ret
	}
	return string(f.Bytes[r.Start.Byte:r.End.Byte])
}

// messagesFromDiags extracts useful messages from the supplied diagnostics object.
func (e *Evaluator) messagesFromDiags(d hcl.Diagnostics) []string {
	var ret []string
	for _, diag := range d {
		var parts []string
		var r *hcl.Range
		if diag.Expression != nil {
			r2 := diag.Expression.Range()
			r = &r2
		} else if diag.Context != nil {
			r = diag.Context
		}
		if r != nil {
			parts = append(parts, e.sourceCode(*r))
		}
		parts = append(parts, diag.Error())
		ret = append(ret, strings.Join(parts, ", "))
	}
	return ret
}
