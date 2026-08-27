package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

func TestGoccyMultiDocumentSerializationBoundaries(t *testing.T) {
	raw := []byte("cron: '@daily'\n---\nsource:\n  any:\n    - url: https://one.test\n")
	file, err := parser.ParseBytes(raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Docs) != 2 {
		t.Fatalf("initial Docs = %d, want 2", len(file.Docs))
	}
	first, second := file.Docs[0], file.Docs[1]
	t.Logf("initial: count=%d identities=[%p %p] starts=[%v %v] documentStrings=[%q %q] fileString=%q", len(file.Docs), first, second, first.Start, second.Start, first.String(), second.String(), file.String())

	file.Docs = []*ast.DocumentNode{second, second, first}
	t.Logf("after direct copy: count=%d identities=[%p %p %p] starts=[%v %v %v] documentStrings=[%q %q %q] fileString=%q", len(file.Docs), file.Docs[0], file.Docs[1], file.Docs[2], file.Docs[0].Start, file.Docs[1].Start, file.Docs[2].Start, file.Docs[0].String(), file.Docs[1].String(), file.Docs[2].String(), file.String())

	serialized := strings.Join([]string{second.Body.String(), second.Body.String(), first.Body.String()}, "\n---\n") + "\n"

	decoder := yaml.NewDecoder(strings.NewReader(serialized))
	count := 0
	for {
		var document map[string]interface{}
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if document != nil {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("decoder document count = %d, want 3; serialized=%q", count, serialized)
	}
}

func TestWebUIEditorOpensLosslessMultiDocumentConfig(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep this comment\ncron: '@daily'\nfuture: keep-me\n---\nsource:\n  any:\n    - url: https://example.test\n")
	if err := os.WriteFile(filepath.Join(dir, "multi.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/multi.yml", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("open status = %d: %s", r.Code, r.Body.String())
	}
	var body struct {
		Documents []map[string]interface{} `json:"documents"`
		YAML      map[string]interface{}   `json:"yaml"`
		Warnings  []struct {
			Document int    `json:"document"`
			Path     string `json:"path"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Documents) != 2 {
		t.Fatalf("documents = %#v, want both YAML documents", body.Documents)
	}
	if body.YAML["source"] != string(raw) {
		t.Fatalf("yaml source = %#v, want original bytes exposed losslessly", body.YAML["source"])
	}
	if len(body.Warnings) == 0 || body.Warnings[0].Document != 1 || body.Warnings[0].Path != "future" {
		t.Fatalf("warnings = %#v, want locatable unknown-field warning", body.Warnings)
	}
	if _, exists := body.Documents[0]["future"]; !exists {
		t.Fatalf("unknown field was removed: %#v", body.Documents[0])
	}
	if _, exists := body.Documents[0]["log"]; exists {
		t.Fatalf("opening form injected a default value: %#v", body.Documents[0])
	}
}

func TestWebUIEditorMutatesDocumentsLosslessly(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# first\ncron: '@daily'\nfuture: deleted-with-document\n---\n# second\nsource:\n  any:\n    - url: https://one.test\n    - url: https://two.test\n  future_hoster:\n    custom: keep-me\n")
	if err := os.WriteFile(filepath.Join(dir, "edit.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","confirmed":true,"operations":[{"op":"move-document","from":1,"to":0},{"op":"copy-document","index":0},{"op":"delete-document","index":1},{"op":"add-document","value":{"cron":"@hourly"}},{"op":"move-array","document":0,"path":"source.any","from":1,"to":0},{"op":"copy-array","document":0,"path":"source.any","index":0},{"op":"delete-array","document":0,"path":"source.any","index":1},{"op":"delete-field","document":0,"path":"cron"}]}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/edit.yml", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("edit status = %d: %s", r.Code, r.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "edit.yml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(got))
	var documents []map[string]interface{}
	for {
		var document map[string]interface{}
		if err := decoder.Decode(&document); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if document != nil {
			documents = append(documents, document)
		}
	}
	if len(documents) != 3 {
		t.Fatalf("persisted document count = %d, want 3: %#v", len(documents), documents)
	}
	wantURLs := [][]string{
		{"https://two.test", "https://one.test"},
		{"https://one.test", "https://two.test"},
		nil,
	}
	for index, want := range wantURLs {
		if want == nil {
			if _, exists := documents[index]["source"]; exists {
				t.Fatalf("document %d unexpectedly contains source: %#v", index, documents[index])
			}
			continue
		}
		source, ok := documents[index]["source"].(map[string]interface{})
		if !ok {
			t.Fatalf("document %d source = %#v", index, documents[index]["source"])
		}
		entries, ok := source["any"].([]interface{})
		if !ok {
			t.Fatalf("document %d source.any = %#v", index, source["any"])
		}
		gotURLs := make([]string, 0, len(entries))
		for _, entry := range entries {
			item, ok := entry.(map[string]interface{})
			if !ok {
				t.Fatalf("document %d source.any entry = %#v", index, entry)
			}
			gotURLs = append(gotURLs, item["url"].(string))
		}
		if !reflect.DeepEqual(gotURLs, want) {
			t.Fatalf("document %d URLs = %#v, want %#v", index, gotURLs, want)
		}
	}
	for index, document := range documents {
		if document["future"] == "deleted-with-document" {
			t.Fatalf("deleted document survived at index %d: %#v", index, document)
		}
	}
	if _, exists := documents[0]["source"]; !exists {
		t.Fatalf("first persisted document is not the moved source document: %#v", documents[0])
	}
	if _, exists := documents[1]["source"]; !exists {
		t.Fatalf("second persisted document is not the copied source document: %#v", documents[1])
	}
	if documents[2]["cron"] != "@hourly" {
		t.Fatalf("third persisted document is not the added document: %#v", documents[2])
	}
	for index := 0; index < 2; index++ {
		source, ok := documents[index]["source"].(map[string]interface{})
		if !ok {
			t.Fatalf("document %d source = %#v", index, documents[index]["source"])
		}
		future, ok := source["future_hoster"].(map[string]interface{})
		if !ok || future["custom"] != "keep-me" {
			t.Fatalf("document %d lost future_hoster.custom: %#v", index, source["future_hoster"])
		}
	}
	if !bytes.Contains(got, []byte("custom: keep-me")) {
		t.Fatalf("HTTP edit discarded unknown YAML field: %s", got)
	}
	if !bytes.Contains(got, []byte("# second")) {
		t.Fatalf("preserved document comment was discarded: %s", got)
	}
	if !bytes.Contains(got, []byte("https://two.test")) || bytes.Index(got, []byte("https://two.test")) > bytes.Index(got, []byte("https://one.test")) {
		t.Fatalf("array reorder was not preserved: %s", got)
	}
}

func TestWebUIEditorMutatesOrderedArraysLosslessly(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep\nsource:\n  any:\n    - url: 'https://one.test'\n    - url: 'https://two.test'\n")
	if err := os.WriteFile(filepath.Join(dir, "arrays.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","confirmed":true,"operations":[{"op":"move-array","document":0,"path":"source.any","from":1,"to":0},{"op":"copy-array","document":0,"path":"source.any","index":0},{"op":"delete-array","document":0,"path":"source.any","index":1},{"op":"add-array","document":0,"path":"source.any","value":{"url":"https://three.test"}}]}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/arrays.yml", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("array edit status = %d: %s", r.Code, r.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "arrays.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("# keep")) || !bytes.Contains(got, []byte("url: 'https://two.test'")) {
		t.Fatalf("array edit discarded preserved YAML: %s", got)
	}
	if bytes.Index(got, []byte("https://two.test")) > bytes.Index(got, []byte("https://one.test")) {
		t.Fatalf("array move was not preserved: %s", got)
	}
	if !bytes.Contains(got, []byte("https://three.test")) {
		t.Fatalf("array add was not preserved: %s", got)
	}
}

func TestWebUIEditorRejectsStaleVersionWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("cron: '@daily'\n")
	if err := os.WriteFile(filepath.Join(dir, "conflict.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/conflict.yml", strings.NewReader(`{"version":"stale","confirmed":true,"operations":[{"op":"delete-field","document":0,"path":"cron"}]}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusConflict {
		t.Fatalf("stale edit status = %d, want %d: %s", r.Code, http.StatusConflict, r.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "conflict.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("stale edit changed file: got %q want %q", got, raw)
	}
}

func TestWebUIEditorConflictReturnsCurrentDiskVersion(t *testing.T) {
	dir := t.TempDir()
	baseline := []byte("cron: '@daily'\n")
	current := []byte("cron: '@hourly'\n")
	path := filepath.Join(dir, "conflict-detail.yml")
	if err := os.WriteFile(path, baseline, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	baselineSum := sha256.Sum256(baseline)
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	currentSum := sha256.Sum256(current)

	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/conflict-detail.yml", strings.NewReader(`{"version":"`+hex.EncodeToString(baselineSum[:])+`","confirmed":true,"operations":[{"op":"delete-field","document":0,"path":"cron"}]}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d, want %d: %s", r.Code, http.StatusConflict, r.Body.String())
	}
	var response struct {
		CurrentVersion string `json:"currentVersion"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatalf("conflict response is not JSON: %v: %s", err, r.Body.String())
	}
	if response.CurrentVersion != hex.EncodeToString(currentSum[:]) {
		t.Fatalf("currentVersion=%q, want current disk SHA-256", response.CurrentVersion)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, current) {
		t.Fatalf("conflict overwrote external version: got %q want %q", got, current)
	}
}

func TestWebUIEditorDeletesNestedOptionalFieldLosslessly(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep\nsource:\n  any:\n    - url: 'https://one.test'\n      username: optional\n      future: keep-me\n")
	if err := os.WriteFile(filepath.Join(dir, "nested.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	sum := sha256.Sum256(raw)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/nested.yml", strings.NewReader(`{"version":"`+hex.EncodeToString(sum[:])+`","confirmed":true,"operations":[{"op":"delete-field","document":0,"path":"source.any.0.username"}]}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("delete nested field status = %d: %s", r.Code, r.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "nested.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("username:")) {
		t.Fatalf("cleared optional field remains in YAML: %s", got)
	}
	if !bytes.Contains(got, []byte("# keep")) || !bytes.Contains(got, []byte("future: keep-me")) || !bytes.Contains(got, []byte("url: 'https://one.test'")) {
		t.Fatalf("unmodified YAML formatting/content changed: %s", got)
	}
}

func TestWebUIHandlerListsAndOpensRootConfigs(t *testing.T) {
	dir := t.TempDir()
	yamlBytes := []byte("cron: '0 0 * * *'\nsource:\n  future_hoster:\n    custom: keep-me\n")
	for name, data := range map[string][]byte{
		"alpha.yml":  yamlBytes,
		"beta.yaml":  []byte("cron: '@daily'\n"),
		"ignore.txt": []byte("ignored"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "hidden.yml"), []byte("cron: '@hourly'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResponse.Code, http.StatusOK)
	}
	var list struct {
		Configs []struct {
			Name string `json:"name"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Configs) != 2 || list.Configs[0].Name != "alpha.yml" || list.Configs[1].Name != "beta.yaml" {
		t.Fatalf("configs = %#v, want root YAML files only", list.Configs)
	}

	openRequest := httptest.NewRequest(http.MethodGet, "/api/v1/configs/alpha.yml", nil)
	openResponse := httptest.NewRecorder()
	handler.ServeHTTP(openResponse, openRequest)
	if openResponse.Code != http.StatusOK {
		t.Fatalf("open status = %d, want %d: %s", openResponse.Code, http.StatusOK, openResponse.Body.String())
	}
	var document struct {
		Data     map[string]interface{} `json:"data"`
		Schema   map[string]interface{} `json:"schema"`
		UISchema map[string]interface{} `json:"uiSchema"`
		YAML     map[string]interface{} `json:"yaml"`
		Version  string                 `json:"version"`
	}
	if err := json.Unmarshal(openResponse.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(yamlBytes)
	if document.Version != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("version = %q, want SHA-256 of original bytes", document.Version)
	}
	if document.Data["cron"] != "0 0 * * *" {
		t.Fatalf("data = %#v, want parsed YAML", document.Data)
	}
	if document.Schema["type"] != "object" || len(document.Schema) == 0 {
		t.Fatalf("schema = %#v, want config contract", document.Schema)
	}
	if document.UISchema == nil {
		t.Fatal("uiSchema is missing")
	}
	if document.YAML["file"] != "alpha.yml" {
		t.Fatalf("yaml metadata = %#v, want file name", document.YAML)
	}
}

func TestWebUIHandlerVersionedAPIBoundariesAndSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/api/configs", "/api/v2/configs", "/api/v1/configs/../config.yml", "/api/v1/configs/missing.yml"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code == http.StatusOK {
			t.Fatalf("%s unexpectedly returned 200", path)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("SPA response status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestWebUIListenAddressIsLoopbackOnly(t *testing.T) {
	if got := webUIListenAddress(6175); got != "127.0.0.1:6175" {
		t.Fatalf("listen address = %q, want loopback default port", got)
	}
	if got := webUIListenAddress(7000); got != "127.0.0.1:7000" {
		t.Fatalf("listen address = %q, want overridden loopback port", got)
	}
}

func TestWebUIWorkspaceListsSafeRootConfigsWithMetadata(t *testing.T) {
	dir := t.TempDir()
	good := []byte("cron: '@daily'\n---\nsource:\n  any:\n    - url: https://example.test\n")
	if err := os.WriteFile(filepath.Join(dir, "alpha.yml"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("[invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.yml"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "nested.yml"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "alpha.yml"), filepath.Join(dir, "link.yml")); err != nil {
		if !strings.Contains(err.Error(), "privilege") && !os.IsPermission(err) {
			t.Fatal(err)
		}
	}

	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Code, r.Body.String())
	}
	var body struct {
		Configs []struct {
			Name      string `json:"name"`
			Documents int    `json:"documents"`
			Modified  string `json:"modified"`
			Valid     bool   `json:"valid"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Configs) != 2 {
		t.Fatalf("configs = %#v, want two safe root YAML files", body.Configs)
	}
	if body.Configs[0].Name != "alpha.yml" || body.Configs[0].Documents != 2 || !body.Configs[0].Valid || body.Configs[0].Modified == "" {
		t.Fatalf("alpha metadata = %#v", body.Configs[0])
	}
	if body.Configs[1].Name != "broken.yaml" || body.Configs[1].Valid {
		t.Fatalf("broken metadata = %#v", body.Configs[1])
	}
}

func TestWebUIWorkspaceCreateRenameCopyAndImport(t *testing.T) {
	dir := t.TempDir()
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(r, req)
		return r
	}
	if r := request(http.MethodPost, "/api/v1/configs", `{"name":"blank.yml","template":"blank"}`); r.Code != http.StatusCreated {
		t.Fatalf("create blank = %d: %s", r.Code, r.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(dir, "blank.yml")); err != nil || bytes.Contains(data, []byte("\r\n")) {
		t.Fatalf("blank file data=%q err=%v", data, err)
	}
	if r := request(http.MethodPost, "/api/v1/configs", `{"name":"example.yaml","template":"example"}`); r.Code != http.StatusCreated {
		t.Fatalf("create example = %d: %s", r.Code, r.Body.String())
	}
	if r := request(http.MethodPost, "/api/v1/configs", `{"name":"BLANK.YML","template":"blank"}`); r.Code != http.StatusConflict {
		t.Fatalf("case conflict = %d", r.Code)
	}
	if r := request(http.MethodPost, "/api/v1/configs/blank.yml/rename", `{"name":"../escape.yml"}`); r.Code < 400 {
		t.Fatalf("unsafe rename = %d", r.Code)
	}
	if r := request(http.MethodPost, "/api/v1/configs/blank.yml/rename", `{"name":"renamed.yml"}`); r.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", r.Code, r.Body.String())
	}
	if r := request(http.MethodPost, "/api/v1/configs/renamed.yml/copy", `{"name":"copy.yml"}`); r.Code != http.StatusCreated {
		t.Fatalf("copy = %d: %s", r.Code, r.Body.String())
	}
	if r := request(http.MethodPost, "/api/v1/configs/import", `{"name":"import.yml","content":"cron: '@daily'\r\n"}`); r.Code != http.StatusCreated {
		t.Fatalf("import = %d: %s", r.Code, r.Body.String())
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "import.yml")); string(data) != "cron: '@daily'\n" {
		t.Fatalf("import contents = %q, want LF", data)
	}
}

func TestWebUIWorkspaceImportAndExportEnforceEncodingAndSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "export.yml"), []byte("cron: '@daily'\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/export.yml/export", nil))
	if r.Code != http.StatusOK || r.Body.String() != "cron: '@daily'\r\n" {
		t.Fatalf("export status=%d body=%q", r.Code, r.Body.String())
	}
	badUTF8 := append([]byte(`{"name":"bad.yml","content":"`), 0xff)
	badUTF8 = append(badUTF8, []byte(`"}`)...)
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/configs/import", bytes.NewReader(badUTF8)))
	if r.Code < 400 {
		t.Fatalf("invalid UTF-8 import = %d", r.Code)
	}
	tooLarge := bytes.Repeat([]byte("a"), 5*1024*1024+1)
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/configs/import", bytes.NewReader(tooLarge)))
	if r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize request = %d, want 413", r.Code)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.yml"), tooLarge, 0o600); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/large.yml", nil))
	if r.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize file = %d, want 413", r.Code)
	}
}

func TestWebUIWorkspaceExportRejectsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "invalid-utf8.yml"), []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/invalid-utf8.yml/export", nil))
	if r.Code < 400 {
		t.Fatalf("invalid UTF-8 export status = %d, want rejection", r.Code)
	}
}

func TestWebUIWorkspaceRejectsUnsafeFileTargets(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.yml"), []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.yml")); err != nil && !strings.Contains(err.Error(), "privilege") && !os.IsPermission(err) {
		t.Fatal(err)
	}

	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/configs/.hidden.yml",
		"/api/v1/configs/plain.txt",
		"/api/v1/configs/..%2Foutside.yml",
		"/api/v1/configs/%2Foutside.yml",
		"/api/v1/configs/link.yml",
	} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code == http.StatusOK {
			t.Fatalf("unsafe target %q unexpectedly returned 200", path)
		}
	}
}

func TestWebUISaveRequiresVersionAndConfirmation(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep\r\ncron: '@daily'\r\n")
	path := filepath.Join(dir, "save.yml")
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	version := hex.EncodeToString(sum[:])

	for _, body := range []string{
		`{"version":"","confirmed":true,"operations":[{"op":"delete-field","document":0,"path":"cron"}]}`,
		`{"version":"` + version + `","confirmed":false,"operations":[{"op":"delete-field","document":0,"path":"cron"}]}`,
	} {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/save.yml", strings.NewReader(body))
		handler.ServeHTTP(r, req)
		if r.Code < 400 {
			t.Fatalf("unconfirmed/versionless save status = %d", r.Code)
		}
		got, _ := os.ReadFile(path)
		if !bytes.Equal(got, raw) {
			t.Fatalf("rejected save changed file: %q", got)
		}
	}
}

func TestWebUISavePreservesCRLFModeAndCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep\r\ncron: '@daily'\r\nfuture: keep-me\r\n")
	path := filepath.Join(dir, "save.yml")
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","confirmed":true,"operations":[{"op":"delete-field","document":0,"path":"cron"}]}`
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPatch, "/api/v1/configs/save.yml", strings.NewReader(body)))
	if r.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", r.Code, r.Body.String())
	}
	got, _ := os.ReadFile(path)
	if bytes.Contains(got, []byte("cron:")) || !bytes.Contains(got, []byte("\r\n")) || bytes.Contains(bytes.ReplaceAll(got, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("saved YAML did not preserve CRLF/delete: %q", got)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || !bytes.Equal(backup, raw) {
		t.Fatalf("backup=%q err=%v, want previous bytes", backup, err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if os.PathSeparator != '\\' && info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v, want 0640", info.Mode().Perm())
	}
}

func TestWebUIBackupDiffAndRestoreAreReversible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restore.yml")
	current := []byte("cron: '@hourly'\n")
	previous := []byte("cron: '@daily'\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", previous, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/restore.yml/backup", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "@daily") || !strings.Contains(r.Body.String(), "@hourly") {
		t.Fatalf("backup diff status=%d body=%q", r.Code, r.Body.String())
	}

	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/configs/restore.yml/backup/restore", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%q", r.Code, r.Body.String())
	}
	got, _ := os.ReadFile(path)
	backup, _ := os.ReadFile(path + ".bak")
	if !bytes.Equal(got, previous) || !bytes.Equal(backup, current) {
		t.Fatalf("restore current=%q backup=%q", got, backup)
	}
}

func TestWebUIBackupEndpointReturnsUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup-diff.yml")
	current := []byte("# keep\ncron: '@hourly'\n")
	previous := []byte("# keep\ncron: '@daily'\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", previous, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/backup-diff.yml/backup", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("backup status=%d body=%q", r.Code, r.Body.String())
	}
	var response struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Diff, "--- backup-diff.yml.bak") || !strings.Contains(response.Diff, "+++ backup-diff.yml") || !strings.Contains(response.Diff, "-cron: '@daily'") || !strings.Contains(response.Diff, "+cron: '@hourly'") {
		t.Fatalf("backup diff=%q, want unified previous-to-current diff", response.Diff)
	}
}

func TestWebUIBackupRestoreFailureKeepsCurrentConfigAvailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restore-failure.yml")
	current := []byte("cron: '@hourly'\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".bak", 0o700); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/configs/restore-failure.yml/backup/restore", nil))
	if r.Code < 400 {
		t.Fatalf("restore status=%d, want failure", r.Code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, current) {
		t.Fatalf("failed restore changed current config: got %q want %q", got, current)
	}
}

func TestWebUIBackupRestoreFailureAfterReplacingCurrentRollsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restore-rollback.yml")
	current := []byte("cron: '@hourly'\n")
	previous := []byte("cron: '@daily'\n")
	if err := os.WriteFile(path, current, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", previous, 0o600); err != nil {
		t.Fatal(err)
	}

	oldRename := webUIRename
	webUIRename = func(oldpath, newpath string) error {
		if newpath == path+".bak" {
			return fmt.Errorf("injected backup replacement failure")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { webUIRename = oldRename })

	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/configs/restore-rollback.yml/backup/restore", nil))
	if r.Code < 400 {
		t.Fatalf("restore status=%d, want failure", r.Code)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, current) {
		t.Fatalf("failed restore changed current config: got %q want %q", got, current)
	}
}

func TestWebUITrashDeleteRestoreAndPermanentDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trash.yml")
	if err := os.WriteFile(path, []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/v1/configs/trash.yml", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%q", r.Code, r.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deleted config still exists: %v", err)
	}

	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/trash", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("trash list status=%d body=%q", r.Code, r.Body.String())
	}
	var listed struct {
		Trash []struct {
			ID       string `json:"id"`
			Original string `json:"original"`
		} `json:"trash"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Trash) != 1 || listed.Trash[0].Original != "trash.yml" || listed.Trash[0].ID == "" {
		t.Fatalf("trash entries=%#v", listed.Trash)
	}
	id := listed.Trash[0].ID

	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/trash/"+id+"/restore", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("trash restore status=%d body=%q", r.Code, r.Body.String())
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "cron: '@daily'\n" {
		t.Fatalf("restored=%q err=%v", got, err)
	}

	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/v1/configs/trash.yml", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("second delete status=%d body=%q", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/trash", nil))
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	id = listed.Trash[0].ID
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/v1/trash/"+id, nil))
	if r.Code != http.StatusOK {
		t.Fatalf("permanent delete status=%d body=%q", r.Code, r.Body.String())
	}
}

func TestWebUITrashRepeatedDeleteCreatesUniqueEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repeat.yml")
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(path, []byte("cron: '@daily'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/v1/configs/repeat.yml", nil))
		if r.Code != http.StatusOK {
			t.Fatalf("delete %d status=%d body=%q", i, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/trash", nil))
	var listed struct {
		Trash []struct {
			ID       string `json:"id"`
			Original string `json:"original"`
		} `json:"trash"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Trash) != 2 || listed.Trash[0].ID == listed.Trash[1].ID {
		t.Fatalf("repeated deletes did not create unique trash entries: %#v", listed.Trash)
	}
}

func TestWebUITrashRestoreRejectsCaseInsensitiveNameConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restore-case.yml")
	if err := os.WriteFile(path, []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/v1/configs/restore-case.yml", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%q", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/trash", nil))
	var listed struct {
		Trash []struct {
			ID string `json:"id"`
		} `json:"trash"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Trash) != 1 {
		t.Fatalf("trash entries=%#v", listed.Trash)
	}
	if err := os.WriteFile(filepath.Join(dir, "RESTORE-CASE.YML"), []byte("cron: '@hourly'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/trash/"+listed.Trash[0].ID+"/restore", nil))
	if r.Code != http.StatusConflict {
		t.Fatalf("case-conflicting restore status=%d, want %d: %s", r.Code, http.StatusConflict, r.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "RESTORE-CASE.YML"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "cron: '@hourly'\n" {
		t.Fatalf("case-conflicting restore overwrote existing config: %q", got)
	}
}

func TestWebUITrashIDUsesUTCTimestampAndRandomSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timestamped.yml")
	if err := os.WriteFile(path, []byte("cron: '@daily'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/v1/configs/timestamped.yml", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%q", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/trash", nil))
	var listed struct {
		Trash []struct {
			ID string `json:"id"`
		} `json:"trash"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Trash) != 1 {
		t.Fatalf("trash entries=%#v", listed.Trash)
	}
	if matched, err := regexp.MatchString(`^timestamped\.yml-\d{8}T\d{6}\.\d+Z-[0-9a-f]+$`, listed.Trash[0].ID); err != nil || !matched {
		t.Fatalf("trash id=%q, want UTC timestamp and short random suffix", listed.Trash[0].ID)
	}
}

func TestWebUIOpenMasksSensitiveValues(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("source:\n  github:\n    - token: super-secret\n")
	if err := os.WriteFile(filepath.Join(dir, "secret.yml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/configs/secret.yml", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("open status=%d body=%q", r.Code, r.Body.String())
	}
	if strings.Contains(r.Body.String(), "super-secret") {
		t.Fatalf("ordinary config response exposed secret: %s", r.Body.String())
	}
}

func TestWebUIReviewReturnsDraftWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("cron: '@daily'\n")
	path := filepath.Join(dir, "review.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","operations":[{"op":"delete-field","document":0,"path":"cron"}]}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/configs/review.yml/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%q", r.Code, r.Body.String())
	}
	var response struct {
		YAML   string   `json:"yaml"`
		Diff   string   `json:"diff"`
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(response.YAML, "cron:") || response.Diff == "" {
		t.Fatalf("review response=%#v", response)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("review wrote config: got %q want %q", got, raw)
	}
}

func TestWebUIReviewReturnsUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("# keep\ncron: '@daily'\n")
	path := filepath.Join(dir, "diff.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","operations":[{"op":"delete-field","document":0,"path":"cron"}]}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/configs/diff.yml/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%q", r.Code, r.Body.String())
	}
	var response struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Diff, "--- diff.yml") || !strings.Contains(response.Diff, "+++ diff.yml (draft)") || !strings.Contains(response.Diff, "-cron: '@daily'") {
		t.Fatalf("diff=%q, want unified diff with file headers and removed line", response.Diff)
	}
}

func TestWebUIReviewRejectsInvalidCronWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("cron: '@daily'\n")
	path := filepath.Join(dir, "invalid.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","operations":[{"op":"add-document","value":{"cron":"not a cron"}}]}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/configs/invalid.yml/review", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%q", r.Code, r.Body.String())
	}
	var response struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Valid || len(response.Errors) == 0 {
		t.Fatalf("invalid cron review = %#v", response)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("invalid review changed file: got %q want %q", got, raw)
	}
}

func TestWebUISaveRejectsInvalidCronWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("cron: '@daily'\n")
	path := filepath.Join(dir, "invalid-save.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	body := `{"version":"` + hex.EncodeToString(sum[:]) + `","confirmed":true,"operations":[{"op":"add-document","value":{"cron":"not a cron"}}]}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/invalid-save.yml", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(r, req)
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid save status=%d, want %d: %s", r.Code, http.StatusUnprocessableEntity, r.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("invalid save changed file: got %q want %q", got, raw)
	}
}

func TestWebUIReviewAndSaveRejectInvalidConfigurationShape(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("cron: '@daily'\n")
	path := filepath.Join(dir, "invalid-shape.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	version := hex.EncodeToString(sum[:])
	operation := `{"op":"add-document","value":{"cron":123}}`

	review := httptest.NewRecorder()
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/configs/invalid-shape.yml/review", strings.NewReader(`{"version":"`+version+`","operations":[`+operation+`]}`))
	reviewRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(review, reviewRequest)
	if review.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%q", review.Code, review.Body.String())
	}
	var reviewResponse struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(review.Body.Bytes(), &reviewResponse); err != nil {
		t.Fatal(err)
	}
	if reviewResponse.Valid || len(reviewResponse.Errors) == 0 {
		t.Fatalf("invalid shape review=%#v", reviewResponse)
	}

	save := httptest.NewRecorder()
	saveRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/invalid-shape.yml", strings.NewReader(`{"version":"`+version+`","confirmed":true,"operations":[`+operation+`]}`))
	saveRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(save, saveRequest)
	if save.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid shape save status=%d, want %d: %s", save.Code, http.StatusUnprocessableEntity, save.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("invalid shape save changed file: got %q want %q", got, raw)
	}
}

func TestWebUIReviewAndSaveRejectInvalidNativeConfiguration(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("cron: '@daily'\n")
	path := filepath.Join(dir, "invalid-native.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := newWebUIHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	version := hex.EncodeToString(sum[:])
	operation := `{"op":"add-document","value":{"source":{"github":[{"filter":{"lastactivity":"nonsense"}}]}}}`

	review := httptest.NewRecorder()
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/configs/invalid-native.yml/review", strings.NewReader(`{"version":"`+version+`","operations":[`+operation+`]}`))
	reviewRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(review, reviewRequest)
	if review.Code != http.StatusOK {
		t.Fatalf("review status=%d body=%q", review.Code, review.Body.String())
	}
	var reviewResponse struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(review.Body.Bytes(), &reviewResponse); err != nil {
		t.Fatal(err)
	}
	if reviewResponse.Valid || len(reviewResponse.Errors) == 0 {
		t.Fatalf("invalid native configuration review=%#v", reviewResponse)
	}

	save := httptest.NewRecorder()
	saveRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/configs/invalid-native.yml", strings.NewReader(`{"version":"`+version+`","confirmed":true,"operations":[`+operation+`]}`))
	saveRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(save, saveRequest)
	if save.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid native configuration save status=%d, want %d: %s", save.Code, http.StatusUnprocessableEntity, save.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("invalid native configuration save changed file: got %q want %q", got, raw)
	}
}

func TestWebUIEmbeddedSPAAssetsAndRouteFallback(t *testing.T) {
	handler, err := newWebUIHandler(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d: %s", index.Code, http.StatusOK, index.Body.String())
	}
	if contentType := index.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("index Content-Type = %q, want text/html", contentType)
	}

	assetPattern := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+\.(?:js|css))"`)
	assets := assetPattern.FindAllStringSubmatch(index.Body.String(), -1)
	if len(assets) < 2 {
		t.Fatalf("index references %d JS/CSS assets, want at least 2: %s", len(assets), index.Body.String())
	}
	for _, asset := range assets {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset[1], nil))
		if response.Code != http.StatusOK {
			t.Fatalf("asset %s status = %d, want %d", asset[1], response.Code, http.StatusOK)
		}
		if response.Body.Len() == 0 {
			t.Fatalf("asset %s returned an empty body", asset[1])
		}
		contentType := response.Header().Get("Content-Type")
		if strings.HasSuffix(asset[1], ".js") && !strings.Contains(contentType, "javascript") {
			t.Fatalf("JavaScript asset %s Content-Type = %q", asset[1], contentType)
		}
		if strings.HasSuffix(asset[1], ".css") && !strings.Contains(contentType, "text/css") {
			t.Fatalf("CSS asset %s Content-Type = %q", asset[1], contentType)
		}
	}

	spaRoute := httptest.NewRecorder()
	handler.ServeHTTP(spaRoute, httptest.NewRequest(http.MethodGet, "/configs/example/edit", nil))
	if spaRoute.Code != http.StatusOK {
		t.Fatalf("SPA route status = %d, want %d", spaRoute.Code, http.StatusOK)
	}
	if spaRoute.Body.String() != index.Body.String() {
		t.Fatal("SPA route did not fall back to the embedded index")
	}

	apiRoute := httptest.NewRecorder()
	handler.ServeHTTP(apiRoute, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
	if apiRoute.Code != http.StatusNotFound {
		t.Fatalf("unknown API route status = %d, want %d", apiRoute.Code, http.StatusNotFound)
	}
	if strings.Contains(apiRoute.Body.String(), "<html") {
		t.Fatal("unknown API route was incorrectly handled by the SPA fallback")
	}
}
