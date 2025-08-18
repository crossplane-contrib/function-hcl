// Package evaluator implements the HCL processing need to create resource definitions.
package evaluator

import (
	"fmt"
	"strings"

	"github.com/crossplane-contrib/function-hcl/internal/evaluator/functions"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/locals"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	fn "github.com/crossplane/function-sdk-go"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/types/known/structpb"
)

// set up some type aliases for nicer looking code.

type (
	Object        = map[string]any
	DynamicObject = map[string]cty.Value
)

// keys under req, gotten from the run function request.
const (
	reqContext             = "context"
	reqComposite           = "composite"
	reqCompositeConnection = "composite_connection"
	reqObservedResource    = "resource"
	reqObservedConnection  = "connection"
	reqObservedResources   = "resources"
	reqObservedConnections = "connections"
	reqExtraResources      = "extra_resources"
)

// supported blocks and attributes.
const (
	blockGroup     = "group"
	blockResource  = "resource"
	blockResources = "resources"
	blockComposite = "composite"
	blockContext   = "context"
	blockLocals    = locals.BlockLocals
	blockTemplate  = "template"
	blockReady     = "ready"
	blockFunction  = functions.BlockFunction
	blockArg       = functions.BlockArg

	attrBody      = "body"
	attrCondition = "condition"
	attrForEach   = "for_each"
	attrName      = "name"
	attrKey       = "key"
	attrValue     = "value"

	blockLabelStatus     = "status"
	blockLabelConnection = "connection"
)

const (
	reservedReq  = "req"
	reservedSelf = "self"
	reservedArg  = "arg"
)

// automatic annotations we will add to resources that are created in a for_each loop.
const (
	annotationBaseName = "hcl.fn.crossplane.io/collection-base-name"
	annotationIndex    = "hcl.fn.crossplane.io/collection-index"
)

// dynamic names set by the evaluator.
const (
	selfName                = "name"
	selfBaseName            = "basename"
	selfObservedResource    = "resource"
	selfObservedConnection  = "connection"
	selfObservedResources   = "resources"
	selfObservedConnections = "connections"
	iteratorName            = "each"
)

// DiscardType describes what was discarded by the function.
type DiscardType string

const (
	discardTypeResource     DiscardType = "resource"
	discardTypeResourceList DiscardType = "resources"
	discardTypeGroup        DiscardType = "group"
	discardTypeStatus       DiscardType = "composite-status"
	discardTypeConnection   DiscardType = "composite-connection"
	discardTypeReady        DiscardType = "resource-ready"
	discardTypeContext      DiscardType = "context"
)

// DiscardReason describes the reason for the elision.
type DiscardReason string

// discard reasons.
const (
	discardReasonUserCondition DiscardReason = "user-condition"
	discardReasonIncomplete    DiscardReason = "incomplete"
	discardReasonBadSecret     DiscardReason = "bad-secret"
)

// File is an HCL file to evaluate.
type File struct {
	Name    string // the name is informational and only used in diagnostic messages
	Content string // the content is the HCL content as a byte-array
}

// Options are evaluation options.
type Options struct {
	Logger logging.Logger
	Debug  bool
}

// DiscardItem is an instance of a resource, resource list, group, connection detail or a composite status
// being discarded from the output either based on user conditions or an incomplete definition of the
// object in question.
type DiscardItem struct {
	Type        DiscardType   `json:"type"`                  // the kind of thing that is discarded
	Reason      DiscardReason `json:"reason"`                // the reason for the discard
	Name        string        `json:"name,omitempty"`        // used only for things that are named
	SourceRange string        `json:"sourceRange,omitempty"` // source range where the discard happened
	Context     []string      `json:"context,omitempty"`     // relevant messages with more details
}

func (di DiscardItem) MessageString() string {
	base := []string{fmt.Sprintf("%s:discarded %s %s", di.SourceRange, di.Type, di.Name)}
	base = append(base, di.Context...)
	return strings.Join(base, "\n")
}

// Evaluator evaluates the HCL DSL created for the purposes of producing crossplane resources.
// Evaluators have mutable state and must not be re-used, nor are they safe for concurrent use.
type Evaluator struct {
	log                      logging.Logger              // the logger to use
	debug                    bool                        // whether we are in debug mode
	files                    map[string]*hcl.File        // map of HCL files keyed by source filename
	existingResourceMap      DynamicObject               // tracks resource names present in observed resources
	existingConnectionMap    DynamicObject               // tracks observed resource connection details.
	collectionResourcesMap   DynamicObject               // tracks resource names present in observed resource collections
	collectionConnectionsMap DynamicObject               // tracks observed collection resource connection details.
	desiredResources         map[string]*structpb.Struct // desired resource bodies
	compositeStatuses        []Object                    // status attributes of the composite
	compositeConnections     []map[string][]byte         // composite connection details
	contexts                 []Object                    // desired context values
	ready                    map[string]int32            // readiness indicator for resource
	discards                 []DiscardItem               // list of things discarded from output
}

// New creates an evaluator.
func New(opts Options) (*Evaluator, error) {
	if opts.Logger == nil {
		var err error
		opts.Logger, err = fn.NewLogger(opts.Debug)
		if err != nil {
			return nil, err
		}
	}
	return &Evaluator{
		log:              opts.Logger,
		debug:            opts.Debug,
		files:            map[string]*hcl.File{},
		desiredResources: map[string]*structpb.Struct{},
		ready:            map[string]int32{},
	}, nil
}

// Eval evaluates the supplied HCL files. Ordering of these files are not important for evaluation.
// Internally they are just processed as though all the files were concatenated into a single file.
func (e *Evaluator) Eval(in *fnv1.RunFunctionRequest, files ...File) (*fnv1.RunFunctionResponse, error) {
	return e.doEval(in, files...)
}

// Analyze runs static checks on the supplied HCL files that implement a composition.
// It returns errors and warnings in the process.
func (e *Evaluator) Analyze(files ...File) hcl.Diagnostics {
	return e.doAnalyze(files...)
}
