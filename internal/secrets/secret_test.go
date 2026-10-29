package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const canary = "canary-7f3a9c-hunter2"

func TestSEC_MEM_001_DestroyZeroesBacking(t *testing.T) {
	backing := []byte(canary)
	alias := backing[:len(backing):len(backing)]
	s := NewSecret(backing)
	cp := s // copies share the buffer
	if got := string(s.Bytes()); got != canary {
		t.Fatalf("Bytes = %q", got)
	}
	if s.Len() != len(canary) {
		t.Fatalf("Len = %d", s.Len())
	}
	s.Destroy()
	if !bytes.Equal(alias, make([]byte, len(canary))) {
		t.Fatalf("backing array not zeroed: %v", alias)
	}
	if s.Bytes() != nil || s.Len() != 0 {
		t.Fatal("destroyed secret still has bytes")
	}
	if cp.Bytes() != nil {
		t.Fatal("copy still has bytes after Destroy")
	}
	s.Destroy() // idempotent

	var zero Secret
	zero.Destroy()
	if zero.Bytes() != nil || zero.Len() != 0 {
		t.Fatal("zero Secret not empty")
	}
}

// allowedStringResults lists the exported functions of package secrets that
// may return a string. None of them returns secret material.
var allowedStringResults = map[string]bool{
	"Secret.String":   true, // "[REDACTED]"
	"Secret.GoString": true, // "[REDACTED]"
	"Ref.Account":     true, // "<uuid>/<field>", not secret
}

func TestSEC_MEM_001_NoStringSecretAPI(t *testing.T) {
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("secrets", fset, files, nil)
	if err != nil {
		t.Fatal(err)
	}

	returnsString := func(sig *types.Signature) bool {
		for v := range sig.Results().Variables() {
			if b, ok := v.Type().Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 {
				return true
			}
		}
		return false
	}
	checked := 0
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		if fn, ok := obj.(*types.Func); ok {
			checked++
			if returnsString(fn.Signature()) && !allowedStringResults[name] {
				t.Errorf("exported func %s returns a string", name)
			}
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		ms := types.NewMethodSet(types.NewPointer(tn.Type()))
		for sel := range ms.Methods() {
			m := sel.Obj().(*types.Func)
			if !m.Exported() {
				continue
			}
			checked++
			key := name + "." + m.Name()
			if returnsString(m.Signature()) && !allowedStringResults[key] {
				t.Errorf("exported method %s returns a string", key)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d functions; is the scan broken?", checked)
	}
}

type logged struct {
	Name string
	Pass Secret
	priv Secret
	Ptr  *Secret
}

func TestSEC_LOG_002_RedactedEveryFormatter(t *testing.T) {
	newS := func() Secret { return NewSecret([]byte(canary)) }
	s := newS()
	p := newS()
	wrap := logged{Name: "x", Pass: newS(), priv: newS(), Ptr: &p}

	outputs := map[string]string{}
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%X", "%d", "%T"} {
		outputs["fmt "+verb] = fmt.Sprintf(verb, s)
		outputs["fmt ptr "+verb] = fmt.Sprintf(verb, &s)
		outputs["fmt struct "+verb] = fmt.Sprintf(verb, wrap)
		outputs["fmt slice "+verb] = fmt.Sprintf(verb, []Secret{s})
	}
	outputs["Sprint"] = fmt.Sprint(s)
	outputs["Sprintln"] = fmt.Sprintln(s)
	outputs["String"] = s.String()
	outputs["GoString"] = s.GoString()
	outputs["error wrap"] = fmt.Errorf("failed with %v", s).Error()

	for name, v := range map[string]any{"value": s, "struct": wrap, "map": map[string]Secret{"k": s}} {
		j, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		outputs["json "+name] = string(j)
	}
	mk, err := json.Marshal(map[Secret]int{s: 1})
	if err != nil {
		t.Fatal(err)
	}
	outputs["json map key"] = string(mk)

	for _, h := range []struct {
		name string
		new  func(*bytes.Buffer) slog.Handler
	}{
		{"text", func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) }},
		{"json", func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) }},
	} {
		var buf bytes.Buffer
		l := slog.New(h.new(&buf))
		l.Info("auth", "secret", s, "wrap", wrap, slog.Any("any", s), slog.Group("g", "s", s))
		outputs["slog "+h.name] = buf.String()
	}

	// decimalBytes is how fmt prints the raw []byte, e.g. "[99 97 110 ...]".
	nums := make([]string, len(canary))
	for i := range len(canary) {
		nums[i] = strconv.Itoa(int(canary[i]))
	}
	decimalBytes := "[" + strings.Join(nums, " ") + "]"

	for name, out := range outputs {
		if strings.Contains(out, canary) {
			t.Errorf("%s leaks the secret: %s", name, out)
		}
		if strings.Contains(out, decimalBytes) || strings.Contains(out, fmt.Sprintf("%x", canary)) {
			t.Errorf("%s leaks the secret bytes: %s", name, out)
		}
		if !strings.Contains(name, "%T") && !strings.Contains(out, redacted) {
			t.Errorf("%s does not say %s: %s", name, redacted, out)
		}
	}
	if got := fmt.Sprintf("%v|%s|%#v|%q", s, s, s, s); got != "[REDACTED]|[REDACTED]|[REDACTED]|[REDACTED]" {
		t.Errorf("formatted = %q", got)
	}
	if v := s.LogValue(); v.Kind() != slog.KindString || v.String() != redacted {
		t.Errorf("LogValue = %v", v)
	}
	if b, _ := s.MarshalText(); string(b) != redacted {
		t.Errorf("MarshalText = %q", b)
	}
}
