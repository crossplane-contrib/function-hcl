package fn

import (
	"context"
	"fmt"

	input "github.com/crossplane-contrib/function-hcl/input/v1beta1"
	"github.com/crossplane-contrib/function-hcl/internal/evaluator"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/function-sdk-go"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/pkg/errors"
	"golang.org/x/tools/txtar"
	"google.golang.org/protobuf/types/known/structpb"
)

const debugAnnotation = "hcl.fn.crossplane.io/debug"

// Options are options for the cue runner.
type Options struct {
	Logger logging.Logger
	Debug  bool
}

type Fn struct {
	fnv1.UnimplementedFunctionRunnerServiceServer
	log   logging.Logger
	debug bool
}

// New creates a hcl runner.
func New(opts Options) (*Fn, error) {
	if opts.Logger == nil {
		var err error
		opts.Logger, err = function.NewLogger(opts.Debug)
		if err != nil {
			return nil, err
		}
	}
	return &Fn{
		log:   opts.Logger,
		debug: opts.Debug,
	}, nil
}

// RunFunction runs the function.
func (f *Fn) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (outRes *fnv1.RunFunctionResponse, finalErr error) {
	// setup response with desired state set up upstream functions
	res := response.To(req, response.DefaultTTL)

	logger := f.log
	// automatically handle errors and response logging
	defer func() {
		if finalErr == nil {
			logger.Info("hcl module executed successfully")
			response.Normal(outRes, "hcl module executed successfully")
			return
		}
		logger.Info(finalErr.Error())
		response.Fatal(res, finalErr)
		outRes = res
	}()

	// setup logging and debugging
	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		return nil, errors.Wrap(err, "get observed composite")
	}
	tag := req.GetMeta().GetTag()
	if tag != "" {
		logger = f.log.WithValues("tag", tag)
	}
	logger = logger.WithValues(
		"xr-version", oxr.Resource.GetAPIVersion(),
		"xr-kind", oxr.Resource.GetKind(),
		"xr-name", oxr.Resource.GetName(),
	)
	logger.Info("Running Function")
	debugThis := false
	annotations := oxr.Resource.GetAnnotations()
	if annotations != nil && annotations[debugAnnotation] == "true" {
		debugThis = true
	}

	// get inputs
	in := &input.HclInput{}
	if err := request.GetInput(req, in); err != nil {
		return nil, errors.Wrap(err, "unable to get input")
	}
	if in.HCL == "" {
		return nil, fmt.Errorf("input HCL was not specified")
	}
	if in.DebugNew {
		if len(req.GetObserved().GetResources()) == 0 {
			debugThis = true
		}
	}

	var files []evaluator.File
	archive := txtar.Parse([]byte(in.HCL))
	for _, file := range archive.Files {
		files = append(files, evaluator.File{Name: file.Name, Content: string(file.Data)})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no HCL input files found, are you using the txtar format?")
	}

	e, err := evaluator.New(evaluator.Options{
		Logger: logger,
		Debug:  debugThis,
	})
	if err != nil {
		return nil, errors.Wrap(err, "create evaluator")
	}

	evalRes, err := e.Eval(req, files...)
	if err != nil {
		return nil, errors.Wrap(err, "evaluate hcl")
	}
	r, err := f.mergeResponse(res, evalRes)
	return r, err
}

func (f *Fn) mergeResponse(res *fnv1.RunFunctionResponse, hclResponse *fnv1.RunFunctionResponse) (*fnv1.RunFunctionResponse, error) {
	if res.Desired == nil {
		res.Desired = &fnv1.State{}
	}
	if res.Desired.Resources == nil {
		res.Desired.Resources = map[string]*fnv1.Resource{}
	}

	// only set desired composite if the evaluator script actually returns it.
	// we assume that the evaluator sets the `status` attribute only.
	if hclResponse.Desired.GetComposite() != nil {
		res.Desired.Composite = hclResponse.Desired.GetComposite()
	}

	// set desired resources from hcl output
	for k, v := range hclResponse.Desired.GetResources() {
		res.Desired.Resources[k] = v
	}

	// merge the context if hclResponse has something in it
	if hclResponse.Context != nil {
		ctxMap := map[string]interface{}{}
		// set up base map, if found
		if res.Context != nil {
			ctxMap = res.Context.AsMap()
		}
		// merge values from hclResponse
		for k, v := range hclResponse.Context.AsMap() {
			ctxMap[k] = v
		}
		s, err := structpb.NewStruct(ctxMap)
		if err != nil {
			return nil, errors.Wrap(err, "set response context")
		}
		res.Context = s
	}

	res.Results = hclResponse.Results
	res.Conditions = hclResponse.Conditions
	res.Requirements = hclResponse.Requirements
	return res, nil
}
