package schema

import (
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/schema"
	"github.com/zclconf/go-cty/cty"
)

var std map[string]*schema.BodySchema

func basicK8sObjectSchema() schema.Object {
	return schema.Object{
		Attributes: map[string]*schema.AttributeSchema{
			"apiVersion": {
				Description:     lang.PlainText("k8s api version"),
				IsRequired:      true,
				Constraint:      schema.String{},
				CompletionHooks: []lang.CompletionHook{{Name: "apiVersion"}},
			},
			"kind": {
				Description:     lang.PlainText("k8s kind"),
				IsRequired:      true,
				Constraint:      schema.String{},
				CompletionHooks: []lang.CompletionHook{{Name: "kind"}},
			},
			"metadata": {
				Description: lang.PlainText("k8s metadata"),
				IsOptional:  true,
				Constraint: schema.Object{
					Attributes: schema.ObjectAttributes{
						"name": {
							Description: lang.Markdown("k8s object name"),
							IsOptional:  true,
							Constraint:  schema.String{},
						},
						"generateName": {
							Description: lang.Markdown("generate k8s object name with random suffix"),
							IsOptional:  true,
							Constraint:  schema.String{},
						},
						"namespace": {
							Description: lang.Markdown("k8s object namespace"),
							IsOptional:  true,
							Constraint:  schema.String{},
						},
						"labels": {
							Description: lang.Markdown("k8s object labels"),
							IsOptional:  true,
							Constraint:  schema.Map{Elem: schema.String{}},
						},
						"annotations": {
							Description: lang.Markdown("k8s object annotations"),
							IsOptional:  true,
							Constraint:  schema.Map{Elem: schema.String{}},
						},
					},
				},
			},
		},
	}
}

func basicBodyAttributeSchema() *schema.AttributeSchema {
	return &schema.AttributeSchema{
		Description: lang.PlainText("K8s object definition"),
		IsRequired:  true,
		Constraint:  basicK8sObjectSchema(),
	}
}

func init() {
	conditionAttributeSchema := func() *schema.AttributeSchema {
		return &schema.AttributeSchema{
			Description: lang.PlainText("condition"),
			IsOptional:  true,
			Constraint:  schema.Bool{},
		}
	}
	localsBlock := func() *schema.BasicBlockSchema {
		return &schema.BasicBlockSchema{
			Description: lang.PlainText("local variables"),
		}
	}
	compositeBlock := func() *schema.BasicBlockSchema {
		return &schema.BasicBlockSchema{
			Description: lang.PlainText("composite status or connection"),
			Labels: []*schema.LabelSchema{
				{
					Name:          "what",
					Description:   lang.PlainText("whether status or connection"),
					AllowedValues: []string{"status", "connection"},
				},
			},
		}
	}
	contextBlock := func() *schema.BasicBlockSchema {
		return &schema.BasicBlockSchema{
			Description: lang.PlainText("assign a value in the context"),
		}
	}
	validationBlock := func() *schema.BasicBlockSchema {
		return &schema.BasicBlockSchema{
			Description: lang.PlainText("fail the evaluation with a message when a condition is false"),
		}
	}
	groupBlocks := func() map[string]*schema.BasicBlockSchema {
		return map[string]*schema.BasicBlockSchema{
			"locals": localsBlock(),
			"group": {
				Description: lang.PlainText("resource group"),
			},
			"resource": {
				Description: lang.PlainText("resource definition"),
				Labels: []*schema.LabelSchema{
					{
						Name:        "name",
						Description: lang.PlainText("crossplane resource name"),
					},
				},
			},
			"resources": {
				Description: lang.PlainText("resource collection definition"),
				Labels: []*schema.LabelSchema{
					{
						Name:        "name",
						Description: lang.PlainText("base name for resource collection"),
					},
				},
			},
			"composite":  compositeBlock(),
			"context":    contextBlock(),
			"validation": validationBlock(),
			"requirement": {
				Description: lang.PlainText("require an existing resource"),
				Labels: []*schema.LabelSchema{
					{
						Name:        "name",
						Description: lang.PlainText("requirement name"),
					},
				},
			},
		}
	}
	topLevelBlocks := func() map[string]*schema.BasicBlockSchema {
		g := groupBlocks()
		g["function"] = &schema.BasicBlockSchema{
			Description: lang.PlainText("function definition"),
			Labels: []*schema.LabelSchema{
				{
					Name:        "name",
					Description: lang.PlainText("function name"),
				},
			},
		}
		return g
	}
	resChildren := func() map[string]*schema.BasicBlockSchema {
		return map[string]*schema.BasicBlockSchema{
			"locals": localsBlock(),
			"ready": {
				Description: lang.PlainText("set ready condition"),
			},
			"composite": compositeBlock(),
			"context":   contextBlock(),
		}
	}

	std = map[string]*schema.BodySchema{
		"": {
			NestedBlocks: topLevelBlocks(),
		},
		"group": {
			Description: lang.PlainText("resource group"),
			Attributes: map[string]*schema.AttributeSchema{
				"condition": conditionAttributeSchema(),
			},
			NestedBlocks: groupBlocks(),
		},
		"locals": {
			Description: lang.PlainText("local variables"),
		},
		"resource": {
			Description: lang.PlainText("resource declaration"),
			Attributes: map[string]*schema.AttributeSchema{
				"condition": conditionAttributeSchema(),
				"body":      basicBodyAttributeSchema(),
			},
			NestedBlocks: resChildren(),
		},
		"template": {
			Description: lang.PlainText("template resource declaration"),
			Attributes: map[string]*schema.AttributeSchema{
				"condition": conditionAttributeSchema(),
				"body":      basicBodyAttributeSchema(),
			},
			NestedBlocks: resChildren(),
		},
		"resources": {
			Description: lang.PlainText("resource collection declaration"),
			Attributes: map[string]*schema.AttributeSchema{
				"condition": conditionAttributeSchema(),
				"for_each": {
					IsOptional:  false,
					Description: lang.Markdown("the collection to iterate over"),
					Constraint:  schema.Any{}, // XXX: make more specific
				},
				"name": {
					IsOptional:  true,
					Description: lang.Markdown("the template for the crossplane name of individual resources"),
					Constraint:  schema.String{},
				},
			},
			NestedBlocks: map[string]*schema.BasicBlockSchema{
				"template": {
					Description: lang.PlainText("template resource definition"),
				},
				"locals":    localsBlock(),
				"composite": compositeBlock(),
				"context":   contextBlock(),
			},
		},
		"composite": {
			Description: lang.PlainText("composite status or connection"),
			Attributes: map[string]*schema.AttributeSchema{
				"body": {
					Description: lang.PlainText("composite status or connection body"),
					IsRequired:  true,
					Constraint: schema.Object{
						Description:           lang.PlainText("composite status or connection object"),
						AllowInterpolatedKeys: false,
						AnyAttribute:          schema.Any{},
					},
				},
			},
			NestedBlocks: map[string]*schema.BasicBlockSchema{
				"locals": localsBlock(),
			},
		},
		"context": {
			Description: lang.PlainText("context value declaration"),
			Attributes: map[string]*schema.AttributeSchema{
				"key": {
					Description: lang.PlainText("context key"),
					IsRequired:  true,
					Constraint:  schema.String{},
				},
				"value": {
					Description: lang.PlainText("context value"),
					IsRequired:  true,
					Constraint:  schema.Any{},
				},
			},
			NestedBlocks: map[string]*schema.BasicBlockSchema{
				"locals": localsBlock(),
			},
		},
		"requirement": {
			Description: lang.PlainText("requirement declaration"),
			Attributes: map[string]*schema.AttributeSchema{
				"condition": conditionAttributeSchema(),
			},
			NestedBlocks: map[string]*schema.BasicBlockSchema{
				"locals": localsBlock(),
				"select": {
					Description: lang.PlainText("selection criteria"),
				},
			},
		},
		"validation": {
			Description: lang.PlainText("input validation"),
			Attributes: map[string]*schema.AttributeSchema{
				"condition": {
					Description: lang.PlainText("must be true, otherwise the evaluation fails with error_message"),
					IsRequired:  true,
					Constraint:  schema.Bool{},
				},
				"error_message": {
					Description: lang.PlainText("the error returned when condition is false"),
					IsRequired:  true,
					Constraint:  schema.String{},
				},
			},
		},
		"select": {
			Description: lang.PlainText("selection"),
			Attributes: map[string]*schema.AttributeSchema{
				"apiVersion": {
					Description: lang.PlainText("k8s api version"),
					IsRequired:  true,
					Constraint:  schema.String{},
				},
				"kind": {
					Description: lang.PlainText("k8s kind"),
					IsRequired:  true,
					Constraint:  schema.String{},
				},
				"matchName": {
					Description: lang.PlainText("k8s object name to match"),
					IsOptional:  true,
					Constraint:  schema.String{},
				},
				"matchLabels": {
					Description: lang.PlainText("k8s labels to match"),
					IsOptional:  true,
					Constraint: schema.Map{
						Name: "label",
						Elem: schema.String{},
					},
				},
			},
		},
		"function": {
			Description: lang.PlainText("function definition"),
			Attributes: map[string]*schema.AttributeSchema{
				"body": {
					Description: lang.PlainText("function body"),
					IsRequired:  true,
					Constraint:  schema.Any{},
				},
				"description": {
					Description: lang.PlainText("function description"),
					IsOptional:  true,
					Constraint:  schema.LiteralType{Type: cty.String},
				},
			},
			NestedBlocks: map[string]*schema.BasicBlockSchema{
				"locals": localsBlock(),
				"arg": {
					Description: lang.PlainText("function argument"),
					Labels: []*schema.LabelSchema{
						{
							Name:        "name",
							Description: lang.PlainText("argument name"),
						},
					},
					AllowMultiple: true,
				},
			},
		},
		"arg": {
			Description: lang.PlainText("argument definition"),
			Attributes: map[string]*schema.AttributeSchema{
				"description": {
					Description: lang.PlainText("argument description"),
					IsOptional:  true,
					Constraint:  schema.LiteralType{Type: cty.String},
				},
				"default": {
					Description: lang.PlainText("default value"),
					IsOptional:  true,
					Constraint:  schema.LiteralType{Type: cty.DynamicPseudoType},
				},
			},
		},
		"ready": {
			Description: lang.PlainText("ready condition"),
			Attributes: map[string]*schema.AttributeSchema{
				"value": {
					Description: lang.PlainText("ready status value (READY_UNSPECIFIED, READY_TRUE, or READY_FALSE)"),
					IsRequired:  true,
					Constraint:  schema.String{},
				},
			},
		},
	}
}
