package evaluator

import (
	"github.com/crossplane-contrib/function-hcl/internal/evaluator/functions"
	"github.com/hashicorp/hcl/v2"
)

// file that declares schemas for various blocks

var (
	// base blocks applicable to top-level as well as groups.
	baseGroupBlocks = []hcl.BlockHeaderSchema{
		{Type: blockLocals},
		{Type: blockGroup},
		{Type: blockResource, LabelNames: []string{"name"}},
		{Type: blockResources, LabelNames: []string{"baseName"}},
		{Type: blockComposite, LabelNames: []string{"object"}},
		{Type: blockContext},
	}

	topBlocks = append(baseGroupBlocks, hcl.BlockHeaderSchema{Type: blockFunction, LabelNames: []string{"name"}})

	// applicable to resource and template blocks.
	resourceBlocks = []hcl.BlockHeaderSchema{
		{Type: blockLocals},
		{Type: blockReady},
		{Type: blockComposite, LabelNames: []string{"object"}},
		{Type: blockContext},
	}
)

var schemasByBlockType = map[string]*hcl.BodySchema{
	blockGroup:     groupSchema(),
	blockResource:  resourceSchema(),
	blockResources: resourcesSchema(),
	blockComposite: compositeSchema(),
	blockContext:   contextSchema(),
	blockTemplate:  templateSchema(),
	blockReady:     readySchema(),
	blockFunction:  functions.FunctionSchema(),
	blockArg:       functions.ArgSchema(),
}

func topLevelSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: topBlocks,
	}
}

func groupSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: baseGroupBlocks,
		Attributes: []hcl.AttributeSchema{
			{Name: attrCondition},
		},
	}
}

func resourcesSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: attrCondition},
			{Name: attrForEach, Required: true},
			{Name: attrName},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: blockLocals},
			{Type: blockComposite, LabelNames: []string{"object"}},
			{Type: blockTemplate},
			{Type: blockContext},
		},
	}
}

func templateSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: attrBody, Required: true},
		},
		Blocks: resourceBlocks,
	}
}

func resourceSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: attrBody, Required: true},
			{Name: attrCondition},
		},
		Blocks: resourceBlocks,
	}
}

func contextSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: blockLocals},
		},
		Attributes: []hcl.AttributeSchema{
			{Name: attrKey, Required: true},
			{Name: attrValue, Required: true},
		},
	}
}

func readySchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: blockLocals},
		},
		Attributes: []hcl.AttributeSchema{
			{Name: attrValue, Required: true},
		},
	}
}

func compositeSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: blockLocals},
		},
		Attributes: []hcl.AttributeSchema{
			{Name: attrBody, Required: true},
		},
	}
}
