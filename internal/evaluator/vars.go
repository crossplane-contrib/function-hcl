package evaluator

import (
	"encoding/json"
	"fmt"
	"sort"

	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/pkg/errors"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type nameIndex struct {
	name  string
	index string
}

func (e *Evaluator) trackBaseNames(observedResources map[string]any) (map[string][]string, error) {
	out := map[string][]nameIndex{}
	for name, res := range observedResources {
		obj, ok := res.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("observed resource %q is not a map[string]any", name)
		}
		annotations, found, err := unstructured.NestedStringMap(obj, "metadata", "annotations")
		if err != nil {
			return nil, errors.Wrap(err, "accessing observed resource annotations")
		}
		if !found || annotations == nil {
			continue
		}
		baseName := annotations[annotationBaseName]
		if baseName == "" {
			continue
		}
		index := annotations[annotationIndex] // we assume it exists if base name does, only affects sorting
		out[baseName] = append(out[baseName], nameIndex{name: name, index: index})
	}
	for _, v := range out {
		sort.Slice(v, func(i, j int) bool {
			return v[i].index < v[j].index
		})
	}
	ret := map[string][]string{}
	for k, v := range out {
		var names []string
		for _, ni := range v {
			names = append(names, ni.name)
		}
		ret[k] = names
	}
	return ret, nil
}

func (e *Evaluator) makeVars(parent *hcl.EvalContext, in *fnv1.RunFunctionRequest) (*hcl.EvalContext, error) {
	// toObject converts a resource to an object after removing managed fields.
	// This cuts the processing time needed to almost half,
	// given that it is a lot of useless processing for getting the implied type of these fields.
	toObject := func(r *fnv1.Resource) Object {
		m := r.GetResource().AsMap()
		unstructured.RemoveNestedField(m, "metadata", "managedFields")
		return m
	}

	observedResourceMap := Object{}
	observedConnectionMap := Object{}
	for name, object := range in.GetObserved().GetResources() {
		observedResourceMap[name] = toObject(object)
		observedConnectionMap[name] = object.GetConnectionDetails()
	}
	extra := Object{}
	for name, res := range in.GetExtraResources() {
		resources := res.GetItems()
		var coll []Object
		for _, resource := range resources {
			coll = append(coll, toObject(resource))
		}
		extra[name] = coll
	}

	baseNameMap, err := e.trackBaseNames(observedResourceMap)
	if err != nil {
		return nil, errors.Wrap(err, "get base collections")
	}

	out := Object{
		reqContext:             in.GetContext().AsMap(),
		reqComposite:           toObject(in.GetObserved().GetComposite()),
		reqCompositeConnection: in.GetObserved().GetComposite().GetConnectionDetails(),
		reqObservedResource:    observedResourceMap,
		reqObservedConnection:  observedConnectionMap,
		reqExtraResources:      extra,
	}
	jsonBytes, err := json.Marshal(out)
	if err != nil {
		return nil, errors.Wrap(err, "marshal variables to json")
	}

	impliedType, err := ctyjson.ImpliedType(jsonBytes)
	if err != nil {
		return nil, errors.Wrap(err, "infer types from json")
	}

	varsValue, err := ctyjson.Unmarshal(jsonBytes, impliedType)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal json")
	}

	topMap := varsValue.AsValueMap()
	e.existingResourceMap = topMap[reqObservedResource].AsValueMap()
	e.existingConnectionMap = topMap[reqObservedConnection].AsValueMap()

	collectionResources := DynamicObject{}
	collectionConnections := DynamicObject{}
	for baseName, resourceNames := range baseNameMap {
		var ctyResources, ctyConnections []cty.Value
		for _, resName := range resourceNames {
			ctyResources = append(ctyResources, e.existingResourceMap[resName])
			ctyConnections = append(ctyConnections, e.existingConnectionMap[resName])
			// make collection resources only accessible from the collection so that
			// we can perform better static analysis of resource name references.
			// If this decision turns out to be a mistake it can be added back
			// but going the other way and removing it later will be impossible.
			delete(e.existingResourceMap, resName)
			delete(e.existingConnectionMap, resName)
		}
		collectionResources[baseName] = cty.TupleVal(ctyResources)
		collectionConnections[baseName] = cty.TupleVal(ctyConnections)
	}
	topMap[reqObservedResources] = cty.ObjectVal(collectionResources)
	topMap[reqObservedConnections] = cty.ObjectVal(collectionConnections)

	// create a basic context with vars
	ctx := parent.NewChild()
	ctx.Variables = DynamicObject{
		reservedReq: cty.ObjectVal(topMap),
	}
	return ctx, err
}
