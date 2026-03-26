package debug

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/ghodss/yaml"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	outputWriter      io.Writer = os.Stderr
	fileRE                      = regexp.MustCompile(`[^a-zA-Z0-9_./-]+`)
	multiUnderscoreRE           = regexp.MustCompile(`_+`)
)

// cleanName replaces invalid characters with underscores to create a safe filename.
// It treats "/" characters as special and replaces them with a double dash "--".
// This allows recovery of context keys from filenames in a bash script, for instance,
// when no other special character than a / is present in a context key.
func cleanName(filename string) string {
	cleaned := fileRE.ReplaceAllString(filename, "_")
	cleaned = multiUnderscoreRE.ReplaceAllString(cleaned, "_")
	return strings.ReplaceAll(cleaned, "/", "--")
}

type Options struct {
	Raw bool
}

type Printer struct {
	opts Options
}

func New(o Options) *Printer {
	return &Printer{opts: o}
}

type object = map[string]any

type bufWriter struct {
	kind     string
	buf      *bytes.Buffer
	firstDoc bool
}

func newBufWriter(kind string) *bufWriter {
	return &bufWriter{
		kind:     kind,
		buf:      bytes.NewBuffer([]byte(fmt.Sprintf("## start %s ##\n", kind))),
		firstDoc: true,
	}
}

func (w *bufWriter) comment(s string) {
	w.buf.WriteString("# ")
	w.buf.WriteString(s)
	w.buf.WriteString("\n")
}

func (w *bufWriter) txtarHeader(s string) {
	w.buf.WriteString("-- ")
	w.buf.WriteString(s)
	w.buf.WriteString(" --\n")
}

func (w *bufWriter) file(file string) {
	w.firstDoc = true
	w.buf.WriteString("\n")
	w.txtarHeader(file)
}

func (w *bufWriter) jsonFile(file string, o any) {
	w.file(file)
	b, _ := json.MarshalIndent(o, "", "  ")
	w.buf.Write(b)
	w.buf.WriteString("\n")
}

func (w *bufWriter) yamlDoc(o object, leadingComment string) {
	if w.firstDoc {
		w.firstDoc = false
	} else {
		w.buf.WriteString("---\n")
	}
	if leadingComment != "" {
		w.comment(leadingComment)
	}
	b, _ := yaml.Marshal(o)
	w.buf.Write(b)
}

func (w *bufWriter) done() error {
	_, _ = fmt.Fprintf(w.buf, "\n## end %s ##\n\n", w.kind)
	log.New(outputWriter, "", 0).Println(w.buf.String())
	return nil
}

func (p *Printer) Request(req *fnv1.RunFunctionRequest) error {
	w := newBufWriter("request")

	// write xr
	comp := p.cleanObject(req.GetObserved().GetComposite().GetResource().AsMap())
	w.file("xr.yaml")
	w.yamlDoc(comp, "")

	// write observed
	w.file("observed.yaml")
	for name, o := range req.GetObserved().GetResources() {
		k := p.cleanObject(o.Resource.AsMap())
		w.yamlDoc(k, fmt.Sprintf("crossplane name: %s", name))
	}

	// write a context JSON file for each key
	c0 := req.GetContext()
	if c0 != nil {
		c := c0.AsMap()
		keys := make([]string, 0, len(c))
		for k := range c {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fileName := fmt.Sprintf("context-%s.json", cleanName(k))
			w.jsonFile(fileName, c[k])
		}
	}

	// write extra resources conditionally
	er := req.GetExtraResources()
	if len(er) > 0 {
		w.file("extra-resources.yaml")
		for name, o := range er {
			w.comment("key: " + name)
			for _, r := range o.GetItems() {
				w.yamlDoc(p.cleanObject(r.Resource.AsMap()), "")
			}
		}
	}
	return w.done()
}

func pavedStr(p *fieldpath.Paved, path string) string {
	ret, _ := p.GetString(path)
	return ret
}

func setLabelOrAnnotation(p *fieldpath.Paved, elem, name, value string) error {
	m, err := p.GetValue("metadata")
	if err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}
	if _, ok := m.(object); !ok {
		return fmt.Errorf("metadata was not an object")
	}
	path := "metadata." + elem
	bag, err := p.GetStringObject(path)
	if err != nil && fieldpath.IsNotFound(err) {
		err = nil
		bag = map[string]string{}
	}
	if err != nil {
		return err
	}
	bag[name] = value
	return p.SetValue(path, bag)
}

func setAnnotation(p *fieldpath.Paved, name string, value string) error {
	return setLabelOrAnnotation(p, "annotations", name, value)
}

func setLabel(p *fieldpath.Paved, name string, value string) error {
	return setLabelOrAnnotation(p, "labels", name, value)
}

func renderConditions(conds []*fnv1.Condition) []object {
	var ret []object

	// render a ready status condition as crossplane would do it
	ret = append(ret, object{
		"type":               "Ready",
		"status":             "True",
		"reason":             "Available",
		"lastTransitionTime": "2024-01-01T00:00:00Z",
	})
	for _, cond := range conds {
		status := "Unspecified"
		switch cond.Status {
		case fnv1.Status_STATUS_CONDITION_TRUE:
			status = "True"
		case fnv1.Status_STATUS_CONDITION_FALSE:
			status = "False"
		case fnv1.Status_STATUS_CONDITION_UNKNOWN:
			status = "Unknown"
		}
		ret = append(ret, object{
			"type":               cond.Type,
			"status":             status,
			"message":            cond.Message,
			"reason":             cond.Reason,
			"lastTransitionTime": "2024-01-01T00:00:00Z",
		})
	}
	return ret
}

func (p *Printer) Response(req *fnv1.RunFunctionRequest, res *fnv1.RunFunctionResponse) error {
	w := newBufWriter("response")

	// get desired xr
	var xr object
	if res.GetDesired().GetComposite() != nil && res.GetDesired().GetComposite().GetResource() != nil {
		xr = res.GetDesired().GetComposite().GetResource().AsMap()
	} else {
		xr = object{}
	}

	// add standard metadata to it
	comp := req.Observed.Composite.Resource.AsMap()
	pavedComp := fieldpath.Pave(comp)
	compName := pavedStr(pavedComp, "metadata.name")
	compNs := pavedStr(pavedComp, "metadata.namespace")

	xr["apiVersion"] = comp["apiVersion"]
	xr["kind"] = comp["kind"]
	meta := object{
		"name": compName,
	}
	if compNs != "" {
		meta["namespace"] = compNs
	}
	xr["metadata"] = meta

	conditions := renderConditions(res.GetConditions())
	if s, ok := xr["status"]; ok {
		s, ok := s.(object)
		if !ok {
			return fmt.Errorf("XR status was not an object")
		}
		s["conditions"] = conditions
	} else {
		xr["status"] = object{"conditions": conditions}
	}

	w.file("rendered.yaml")
	w.yamlDoc(xr, "returned composite status")

	// calculate owner refs for desired resources
	oref := object{
		"apiVersion":         pavedStr(pavedComp, "apiVersion"),
		"kind":               pavedStr(pavedComp, "kind"),
		"name":               compName,
		"blockOwnerDeletion": true,
		"controller":         true,
		"uid":                "", // since we clean the request composite of uid
	}
	if compNs != "" {
		oref["namespace"] = compNs
	}
	ownerRefs := []object{oref}

	// get claim ref, if any
	var cr struct {
		name      string
		namespace string
	}

	err := pavedComp.GetValueInto("spec.claimRef", &cr)
	if err != nil && !fieldpath.IsNotFound(err) {
		return fmt.Errorf("get claimRef: %w", err)
	}

	for name, o := range res.GetDesired().GetResources() {
		r := o.Resource.AsMap()
		// mimic what crossplane does after calling the function successfully
		paved := fieldpath.Pave(r)
		if err = paved.SetValue("metadata.generateName", compName+"-"); err != nil {
			return fmt.Errorf("set metadata.generateName: %w", err)
		}
		if err = paved.SetValue("metadata.ownerReferences", ownerRefs); err != nil {
			return fmt.Errorf("set owner references: %w", err)
		}
		if err = setAnnotation(paved, "crossplane.io/composition-resource-name", name); err != nil {
			return fmt.Errorf("set crossplane.io/composition-resource-name annotation: %w", err)
		}
		if err = setLabel(paved, "crossplane.io/composite", compName); err != nil {
			return fmt.Errorf("set crossplane.io/composite annotation: %w", err)
		}
		if cr.name != "" {
			if err = setLabel(paved, "crossplane.io/claim-name", cr.name); err != nil {
				return fmt.Errorf("set crossplane.io/claim-name annotation: %w", err)
			}
			if err = setLabel(paved, "crossplane.io/claim-namespace", cr.namespace); err != nil {
				return fmt.Errorf("set crossplane.io/claim-namespace annotation: %w", err)
			}
		}
		w.yamlDoc(r, "desired object: "+name)
	}
	{
		var ctx object
		if res.GetContext() != nil {
			ctx = res.GetContext().AsMap()
		}
		obj := object{
			"apiVersion": "render.crossplane.io/v1beta1",
			"kind":       "Context",
			"metadata": object{
				"name": "context",
			},
			"fields": ctx,
		}
		w.yamlDoc(obj, "context")
	}
	for i, result := range res.GetResults() {
		obj := object{
			"apiVersion": "render.crossplane.io/v1beta1",
			"kind":       "Result",
			"metadata": object{
				"name": fmt.Sprintf("result-%d", i),
			},
			"message":  result.Message,
			"severity": result.Severity.String(),
			"step":     "run hcl composition",
		}
		w.yamlDoc(obj, "result")
	}

	if res.GetRequirements() != nil && res.GetRequirements().GetExtraResources() != nil {
		w.file("requirements.yaml")
		// do this in two steps because of the weird Match interface that needs protobuf
		b, err := protojson.Marshal(res.GetRequirements())
		if err != nil {
			return fmt.Errorf("marshal requirements: %w", err)
		}
		var er object
		err = json.Unmarshal(b, &er)
		if err != nil {
			return fmt.Errorf("unmarshal requirements: %w", err)
		}
		w.yamlDoc(er, "")
	}
	return w.done()
}

func (p *Printer) cleanObject(k8sObject object) object {
	if p.opts.Raw {
		return k8sObject
	}
	paved := fieldpath.Pave(k8sObject)
	_ = paved.DeleteField(`metadata.annotations["kubectl.kubernetes.io/last-applied-configuration"]`)
	_ = paved.DeleteField(`metadata.managedFields`)
	_ = paved.DeleteField(`metadata.creationTimestamp`)
	_ = paved.DeleteField(`metadata.generation`)
	_ = paved.DeleteField(`metadata.resourceVersion`)
	_ = paved.DeleteField(`metadata.uid`)
	return k8sObject
}
