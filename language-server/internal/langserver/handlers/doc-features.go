package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/crossplane-contrib/function-hcl/function/api"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/document"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/document/diff"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder/completion"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder/folding"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder/semtok"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/funchcl/decoder/symbols"
	"github.com/crossplane-contrib/function-hcl/language-server/internal/langhcl/lang"
	ilsp "github.com/crossplane-contrib/function-hcl/language-server/internal/langserver/lsp"
	lsp "github.com/crossplane-contrib/function-hcl/language-server/internal/langserver/protocol"
	"github.com/hashicorp/hcl/v2"
)

func (svc *service) textDocumentCompletion(ctx context.Context, params lsp.CompletionParams) (lsp.CompletionList, error) {
	var list lsp.CompletionList

	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return list, err
	}
	pos, err := ilsp.HCLPositionFromLspPosition(params.Position, doc)
	if err != nil {
		return list, err
	}
	pc, err := svc.features.modules.PathCompletionContext(path, doc.Filename, pos)
	if err != nil {
		return list, err
	}

	svc.logger.Printf("Looking for candidates at %q -> %#v", doc.Filename, pos)
	c := completion.New(pc)
	candidates, err := c.CompletionAt(doc.Filename, pos)
	if err != nil {
		return list, err
	}

	if behavior := decoder.GetBehavior(); behavior.IndentMultiLineProposals {
		indent := ""
		lineIdx := int(params.Position.Line)
		if lineIdx < len(doc.Lines) {
			for _, b := range doc.Lines[lineIdx].Bytes {
				if b == ' ' || b == '\t' {
					indent += string(b)
				} else {
					break
				}
			}
		}
		if indent != "" {
			for i := range candidates.List {
				if strings.Contains(candidates.List[i].TextEdit.Snippet, "\n") {
					candidates.List[i].TextEdit.Snippet = strings.ReplaceAll(candidates.List[i].TextEdit.Snippet, "\n", "\n"+indent)
				}
				if strings.Contains(candidates.List[i].TextEdit.NewText, "\n") {
					candidates.List[i].TextEdit.NewText = strings.ReplaceAll(candidates.List[i].TextEdit.NewText, "\n", "\n"+indent)
				}
			}
		}
	}

	svc.logger.Printf("received candidates: %#v", candidates)
	out := ilsp.ToCompletionList(candidates, svc.cc.TextDocument)
	return out, err
}

func (svc *service) textDocumentHover(ctx context.Context, params lsp.TextDocumentPositionParams) (*lsp.Hover, error) {
	doc, _, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	pos, err := ilsp.HCLPositionFromLspPosition(params.Position, doc)
	if err != nil {
		return nil, err
	}

	pc, err := svc.features.modules.PathCompletionContext(lang.Path{
		Path:       doc.Dir.Path(),
		LanguageID: doc.LanguageID,
	}, doc.Filename, pos)
	if err != nil {
		return nil, err
	}

	svc.logger.Printf("Looking for hover data at %q -> %#v", doc.Filename, pos)
	c := completion.New(pc)
	hoverData, err := c.HoverAt(doc.Filename, pos)
	svc.logger.Printf("received hover data: %#v", hoverData)
	if err != nil {
		svc.logger.Printf("hover at %q %v failed: %v", doc.Filename, pos, err)
		return nil, nil // hide this from the client
	}
	return ilsp.HoverData(hoverData, svc.cc.TextDocument), nil
}

func (svc *service) textDocumentLink(ctx context.Context, params lsp.DocumentLinkParams) ([]lsp.DocumentLink, error) {
	doc, _, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if doc.LanguageID != ilsp.HCL.String() {
		return nil, nil
	}
	// TODO: implement me
	return nil, nil
}

func (svc *service) textDocumentGoToDefinition(ctx context.Context, params lsp.TextDocumentPositionParams) (interface{}, error) {
	return svc.goToReferenceTarget(ctx, params)
}

func (svc *service) textDocumentGoToDeclaration(ctx context.Context, params lsp.TextDocumentPositionParams) (interface{}, error) {
	return svc.goToReferenceTarget(ctx, params)
}

func (svc *service) goToReferenceTarget(ctx context.Context, params lsp.TextDocumentPositionParams) ([]lsp.LocationLink, error) {
	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	pos, err := ilsp.HCLPositionFromLspPosition(params.Position, doc)
	if err != nil {
		return nil, err
	}
	refMap, err := svc.features.modules.ReferenceMap(path)
	if err != nil {
		return nil, err
	}
	svc.logger.Printf("Looking for definition from %q -> %#v", doc.Filename, pos)
	def := refMap.FindDefinitionFromReference(doc.Filename, pos)
	if def == nil {
		return ilsp.ToLocationLinks(path, []hcl.Range{}), nil
	}
	return ilsp.ToLocationLinks(path, []hcl.Range{*def}), nil
}

func (svc *service) textDocumentReferences(ctx context.Context, params lsp.ReferenceParams) ([]lsp.Location, error) {
	var list []lsp.Location
	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return list, err
	}
	pos, err := ilsp.HCLPositionFromLspPosition(params.Position, doc)
	if err != nil {
		return list, err
	}
	refMap, err := svc.features.modules.ReferenceMap(path)
	if err != nil {
		return nil, err
	}
	svc.logger.Printf("Looking for references from %q -> %#v", doc.Filename, pos)
	refs := refMap.FindReferencesFromDefinition(doc.Filename, pos)
	return ilsp.ToLocations(path, refs), nil
}

func (svc *service) textDocumentSignatureHelp(ctx context.Context, params lsp.SignatureHelpParams) (*lsp.SignatureHelp, error) {
	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	pos, err := ilsp.HCLPositionFromLspPosition(params.Position, doc)
	if err != nil {
		return nil, err
	}
	pc, err := svc.features.modules.PathCompletionContext(path, doc.Filename, pos)
	if err != nil {
		return nil, err
	}
	c := completion.New(pc)
	sig, err := c.SignatureAtPos(doc.Filename, pos)
	if err != nil {
		return nil, err
	}
	return ilsp.ToSignatureHelp(sig), nil
}

func (svc *service) textDocumentSymbol(ctx context.Context, params lsp.DocumentSymbolParams) ([]lsp.DocumentSymbol, error) {
	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	pc, err := svc.features.modules.PathContext(path)
	if err != nil {
		return nil, err
	}
	c := symbols.NewCollector(path)
	syms, err := c.FileSymbols(pc, doc.Filename)
	if err != nil {
		return nil, err
	}
	return ilsp.DocumentSymbols(syms, svc.cc.TextDocument.DocumentSymbol), nil
}

func (svc *service) textDocumentSemanticTokensFull(ctx context.Context, params lsp.SemanticTokensParams) (ret lsp.SemanticTokens, _ error) {
	// TODO: check client capabilities, full request etc.
	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return ret, err
	}
	pc, err := svc.features.modules.PathContext(path)
	if err != nil {
		return ret, err
	}
	tokens, err := semtok.TokensFor(pc, doc.Filename)
	if err != nil {
		return ret, err
	}
	enc := ilsp.NewTokenEncoder(tokens, doc.Lines)
	ret.Data = enc.Encode()
	return ret, nil
}

func (svc *service) textDocumentCodeLens(ctx context.Context, params lsp.CodeLensParams) ([]lsp.CodeLens, error) {
	var list []lsp.CodeLens
	_, _, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return list, err
	}
	/*
		not yet implemented
	*/
	return list, nil
}

func (svc *service) textDocumentFormatting(ctx context.Context, params lsp.DocumentFormattingParams) (_ []lsp.TextEdit, finalErr error) {
	dh := ilsp.HandleFromDocumentURI(params.TextDocument.URI)
	doc, err := svc.docStore.Get(dh)
	if err != nil {
		return nil, err
	}
	return svc.formatDocument(ctx, doc.Text, dh)
}

func (svc *service) formatDocument(ctx context.Context, original []byte, dh document.Handle) ([]lsp.TextEdit, error) {
	formatted := []byte(api.FormatHCL(string(original)))
	if len(formatted) == 0 {
		return nil, fmt.Errorf("format failed")
	}
	changes := diff.Diff(dh, original, formatted)
	return ilsp.TextEditsFromDocumentChanges(changes), nil
}

func (svc *service) textDocumentFoldingRange(ctx context.Context, params lsp.FoldingRangeParams) ([]lsp.FoldingRange, error) {
	doc, path, err := svc.standardInit(ctx, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	pc, err := svc.features.modules.PathContext(path)
	if err != nil {
		return nil, err
	}
	file, ok := pc.HCLFileByName(doc.Filename)
	if !ok {
		return nil, fmt.Errorf("file %s not found", doc.Filename)
	}
	behavior := pc.Behavior()
	ranges := folding.Collect(file, behavior)

	if behavior.InnerBraceRangesForFolding {
		// Adjust end positions: end at the last character of the line before the closing brace.
		// This satisfies lsp4ij's charAt requirements.
		for i := range ranges {
			if ranges[i].EndLine > 1 {
				prevLineIdx := ranges[i].EndLine - 2 // convert to 0-based index for previous line
				if prevLineIdx >= 0 && prevLineIdx < len(doc.Lines) {
					ranges[i].EndLine--
					ranges[i].EndColumn = len(doc.Lines[prevLineIdx].Bytes)
				}
			}
		}
	}

	return ilsp.FoldingRanges(ranges), nil
}
