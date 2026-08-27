package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cooperspencer/gickup/types"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

//go:embed gickup_spec.json conf.example.yml webui/dist
var webUIAssets embed.FS

const webUIMaxConfigSize = 5 * 1024 * 1024

var webUIRename = os.Rename

type webUIConfigInfo struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
	Modified  string `json:"modified"`
	Valid     bool   `json:"valid"`
}

type webUIWarning struct {
	Document int    `json:"document"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

type webUIEditOperation struct {
	Op       string      `json:"op"`
	Document int         `json:"document"`
	Index    int         `json:"index"`
	From     int         `json:"from"`
	To       int         `json:"to"`
	Path     string      `json:"path"`
	Value    interface{} `json:"value"`
}

type webUIEditRequest struct {
	Version    string               `json:"version"`
	Confirmed  bool                 `json:"confirmed"`
	Operations []webUIEditOperation `json:"operations"`
}

func webUIEditYAML(raw []byte, operations []webUIEditOperation) ([]byte, error) {
	file, err := parser.ParseBytes(raw, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, operation := range operations {
		if operation.Op != "delete-field" && operation.Op != "move-document" && operation.Op != "copy-document" && operation.Op != "delete-document" && operation.Op != "add-document" && operation.Op != "move-array" && operation.Op != "copy-array" && operation.Op != "delete-array" && operation.Op != "add-array" {
			return nil, fmt.Errorf("unsupported editor operation %q", operation.Op)
		}
		if operation.Op == "move-document" {
			if operation.From < 0 || operation.From >= len(file.Docs) || operation.To < 0 || operation.To >= len(file.Docs) {
				return nil, fmt.Errorf("invalid document index")
			}
			documents := make([]string, len(file.Docs))
			for i, doc := range file.Docs {
				documents[i] = doc.Body.String()
			}
			doc := documents[operation.From]
			documents = append(documents[:operation.From], documents[operation.From+1:]...)
			documents = append(documents, "")
			copy(documents[operation.To+1:], documents[operation.To:])
			documents[operation.To] = doc
			file, err = parser.ParseBytes([]byte(strings.Join(documents, "\n---\n")), parser.ParseComments)
			if err != nil {
				return nil, err
			}
			continue
		}
		if operation.Op == "copy-document" {
			if operation.Index < 0 || operation.Index >= len(file.Docs) {
				return nil, fmt.Errorf("invalid document index")
			}
			documents := make([]string, 0, len(file.Docs)+1)
			for _, doc := range file.Docs {
				documents = append(documents, doc.Body.String())
			}
			documents = append(documents, file.Docs[operation.Index].Body.String())
			copyFile, err := parser.ParseBytes([]byte(strings.Join(documents, "\n---\n")), parser.ParseComments)
			if err != nil {
				return nil, err
			}
			file = copyFile
			continue
		}
		if operation.Op == "delete-document" {
			if operation.Index < 0 || operation.Index >= len(file.Docs) {
				return nil, fmt.Errorf("invalid document index")
			}
			documents := make([]string, 0, len(file.Docs)-1)
			for i, doc := range file.Docs {
				if i != operation.Index {
					documents = append(documents, doc.Body.String())
				}
			}
			file, err = parser.ParseBytes([]byte(strings.Join(documents, "\n---\n")), parser.ParseComments)
			if err != nil {
				return nil, err
			}
			continue
		}
		if operation.Op == "add-document" {
			value, err := yaml.Marshal(operation.Value)
			if err != nil {
				return nil, err
			}
			documents := make([]string, 0, len(file.Docs)+1)
			for _, doc := range file.Docs {
				documents = append(documents, doc.Body.String())
			}
			documents = append(documents, string(value))
			added, err := parser.ParseBytes([]byte(strings.Join(documents, "\n---\n")), parser.ParseComments)
			if err != nil {
				return nil, err
			}
			if len(added.Docs) != len(documents) {
				return nil, fmt.Errorf("invalid document value")
			}
			file = added
			continue
		}
		if strings.HasSuffix(operation.Op, "-array") {
			if operation.Document < 0 || operation.Document >= len(file.Docs) {
				return nil, fmt.Errorf("invalid document index")
			}
			node, err := webUIYAMLNode(file.Docs[operation.Document].Body, operation.Path)
			if err != nil {
				return nil, err
			}
			sequence, ok := node.(*ast.SequenceNode)
			if !ok {
				return nil, fmt.Errorf("field %q is not an array", operation.Path)
			}
			switch operation.Op {
			case "move-array":
				if operation.From < 0 || operation.From >= len(sequence.Values) || operation.To < 0 || operation.To >= len(sequence.Values) {
					return nil, fmt.Errorf("invalid array index")
				}
				item := sequence.Values[operation.From]
				sequence.Values = append(sequence.Values[:operation.From], sequence.Values[operation.From+1:]...)
				sequence.Values = append(sequence.Values, nil)
				copy(sequence.Values[operation.To+1:], sequence.Values[operation.To:])
				sequence.Values[operation.To] = item
			case "copy-array":
				if operation.Index < 0 || operation.Index >= len(sequence.Values) {
					return nil, fmt.Errorf("invalid array index")
				}
				item, err := webUIParseYAMLNode(sequence.Values[operation.Index].String())
				if err != nil {
					return nil, err
				}
				sequence.Values = append(sequence.Values, nil)
				copy(sequence.Values[operation.Index+2:], sequence.Values[operation.Index+1:])
				sequence.Values[operation.Index+1] = item
			case "delete-array":
				if operation.Index < 0 || operation.Index >= len(sequence.Values) {
					return nil, fmt.Errorf("invalid array index")
				}
				sequence.Values = append(sequence.Values[:operation.Index:operation.Index], sequence.Values[operation.Index+1:]...)
			case "add-array":
				value, err := yaml.Marshal(operation.Value)
				if err != nil {
					return nil, err
				}
				item, err := webUIParseYAMLNode(string(value))
				if err != nil {
					return nil, err
				}
				sequence.Values = append(sequence.Values, item)
			}
			continue
		}
		if operation.Op != "delete-field" {
			return nil, fmt.Errorf("unsupported editor operation %q", operation.Op)
		}
		if operation.Document < 0 || operation.Document >= len(file.Docs) {
			return nil, fmt.Errorf("invalid document index")
		}
		parts := strings.Split(operation.Path, ".")
		var node ast.Node = file.Docs[operation.Document].Body
		for index, part := range parts {
			switch value := node.(type) {
			case *ast.MappingNode:
				found := -1
				for i, entry := range value.Values {
					if entry.Key.String() == part {
						found = i
						break
					}
				}
				if found < 0 {
					break
				}
				if index == len(parts)-1 {
					value.Values = append(value.Values[:found], value.Values[found+1:]...)
					node = nil
				} else {
					node = value.Values[found].Value
				}
			case *ast.SequenceNode:
				var item int
				if _, err := fmt.Sscanf(part, "%d", &item); err != nil || item < 0 || item >= len(value.Values) {
					return nil, fmt.Errorf("invalid array path %q", operation.Path)
				}
				node = value.Values[item]
			default:
				return nil, fmt.Errorf("field %q not found", operation.Path)
			}
			if node == nil {
				break
			}
		}
	}
	return []byte(file.String()), nil
}

func webUIYAMLNode(node ast.Node, path string) (ast.Node, error) {
	for _, part := range strings.Split(path, ".") {
		switch value := node.(type) {
		case *ast.MappingNode:
			found := false
			for _, entry := range value.Values {
				if entry.Key.String() == part {
					node = entry.Value
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("field %q not found", path)
			}
		case *ast.SequenceNode:
			var index int
			if _, err := fmt.Sscanf(part, "%d", &index); err != nil || index < 0 || index >= len(value.Values) {
				return nil, fmt.Errorf("invalid array path %q", path)
			}
			node = value.Values[index]
		default:
			return nil, fmt.Errorf("field %q not found", path)
		}
	}
	return node, nil
}

func webUIParseYAMLNode(raw string) (ast.Node, error) {
	parsed, err := parser.ParseBytes([]byte(raw), parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if len(parsed.Docs) != 1 {
		return nil, fmt.Errorf("invalid YAML value")
	}
	return parsed.Docs[0].Body, nil
}

func webUIParseDocuments(raw []byte, schema map[string]interface{}) ([]map[string]interface{}, []webUIWarning, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	documents := make([]map[string]interface{}, 0)
	warnings := make([]webUIWarning, 0)
	properties, _ := schema["properties"].(map[string]interface{})
	for {
		var document map[string]interface{}
		err := dec.Decode(&document)
		if err == io.EOF {
			return documents, warnings, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if document == nil {
			continue
		}
		documents = append(documents, document)
		for key := range document {
			if _, known := properties[key]; !known {
				warnings = append(warnings, webUIWarning{Document: len(documents), Path: key, Message: "unknown field"})
			}
		}
	}
}

func webUIValidateDraft(raw []byte, schema map[string]interface{}) []string {
	documents, _, err := webUIParseDocuments(raw, schema)
	if err != nil {
		return []string{err.Error()}
	}
	errors := make([]string, 0)
	for index := range documents {
		encoded, err := yaml.Marshal(documents[index])
		if err != nil {
			errors = append(errors, fmt.Sprintf("document %d: %v", index+1, err))
			continue
		}
		var conf types.Conf
		if err := yaml.Unmarshal(encoded, &conf); err != nil {
			errors = append(errors, fmt.Sprintf("document %d: %v", index+1, err))
			continue
		}
		if conf.Cron != "" {
			if _, err := types.ParseCronSpec(conf.Cron); err != nil {
				errors = append(errors, fmt.Sprintf("document %d: invalid cron: %v", index+1, err))
			}
		}
		for _, repo := range conf.Source.Github {
			if err := repo.Filter.ParseDuration(); err != nil {
				errors = append(errors, fmt.Sprintf("document %d: invalid GitHub lastactivity: %v", index+1, err))
			}
		}
	}
	return errors
}

func webUIListenAddress(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func runWebUI(configDir string, port int) error {
	handler, err := newWebUIHandler(configDir)
	if err != nil {
		return err
	}
	address := webUIListenAddress(port)
	fmt.Printf("http://%s\n", address)
	return http.ListenAndServe(address, handler)
}

func newWebUIHandler(configDir string) (http.Handler, error) {
	configRoot, err := filepath.Abs(configDir)
	if err != nil {
		return nil, err
	}
	schemaBytes, err := webUIAssets.ReadFile("gickup_spec.json")
	if err != nil {
		return nil, err
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/configs", func(w http.ResponseWriter, _ *http.Request) {
		entries, err := os.ReadDir(configRoot)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		configs := make([]webUIConfigInfo, 0)
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 || !webUIConfigName(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > webUIMaxConfigSize {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(configRoot, entry.Name()))
			if err != nil || !utf8.Valid(raw) {
				continue
			}
			documents, valid := webUIYAMLDocuments(raw)
			configs = append(configs, webUIConfigInfo{Name: entry.Name(), Documents: documents, Modified: info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"), Valid: valid})
		}
		sort.Slice(configs, func(i, j int) bool { return strings.ToLower(configs[i].Name) < strings.ToLower(configs[j].Name) })
		writeWebUIJSON(w, map[string]interface{}{"configs": configs})
	})
	mux.HandleFunc("POST /api/v1/configs", func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Name, Template string }
		if !webUIDecodeRequest(w, r, &request) || !webUIPrepareTarget(w, configRoot, request.Name) {
			return
		}
		var raw []byte
		switch request.Template {
		case "blank":
			raw = []byte("{}\n")
		case "example":
			raw, err = webUIAssets.ReadFile("conf.example.yml")
		default:
			http.Error(w, "invalid template", http.StatusBadRequest)
			return
		}
		raw = webUILF(raw)
		if len(raw) > webUIMaxConfigSize {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := os.WriteFile(filepath.Join(configRoot, request.Name), raw, 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("POST /api/v1/configs/import", func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Name, Content string }
		if !webUIDecodeRequest(w, r, &request) || !webUIPrepareTarget(w, configRoot, request.Name) {
			return
		}
		raw := webUILF([]byte(request.Content))
		if len(raw) > webUIMaxConfigSize {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := os.WriteFile(filepath.Join(configRoot, request.Name), raw, 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("POST /api/v1/configs/{name}/rename", func(w http.ResponseWriter, r *http.Request) {
		source, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		var request struct{ Name string }
		if !webUIDecodeRequest(w, r, &request) || !webUIPrepareTarget(w, configRoot, request.Name) {
			return
		}
		if err := os.Rename(source, filepath.Join(configRoot, request.Name)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("POST /api/v1/configs/{name}/copy", func(w http.ResponseWriter, r *http.Request) {
		source, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		var request struct{ Name string }
		if !webUIDecodeRequest(w, r, &request) || !webUIPrepareTarget(w, configRoot, request.Name) {
			return
		}
		raw, err := os.ReadFile(source)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(filepath.Join(configRoot, request.Name), raw, 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("GET /api/v1/configs/{name}/export", func(w http.ResponseWriter, r *http.Request) {
		path, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(raw) > webUIMaxConfigSize {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if !utf8.Valid(raw) {
			http.Error(w, "configuration must be UTF-8", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", r.PathValue("name")))
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("GET /api/v1/configs/{name}/backup", func(w http.ResponseWriter, r *http.Request) {
		path, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		current, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		backup, err := os.ReadFile(path + ".bak")
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		diff := webUIUnifiedDiff(r.PathValue("name")+".bak", backup, current)
		diff = strings.Replace(diff, "+++ "+r.PathValue("name")+".bak (draft)", "+++ "+r.PathValue("name"), 1)
		writeWebUIJSON(w, map[string]interface{}{"current": string(current), "backup": string(backup), "diff": diff})
	})
	mux.HandleFunc("POST /api/v1/configs/{name}/backup/restore", func(w http.ResponseWriter, r *http.Request) {
		path, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		current, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		backup, err := os.ReadFile(path + ".bak")
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		info, err := os.Stat(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(path, backup, info.Mode().Perm()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		backupSwap := path + ".bak.restore"
		if err := os.WriteFile(backupSwap, current, info.Mode().Perm()); err != nil {
			_ = os.WriteFile(path, current, info.Mode().Perm())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := webUIRename(backupSwap, path+".bak"); err != nil {
			_ = os.Remove(backupSwap)
			_ = os.WriteFile(path, current, info.Mode().Perm())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/v1/configs/{name}", func(w http.ResponseWriter, r *http.Request) {
		path, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		trashDir := filepath.Join(configRoot, ".gickup-trash")
		if err := os.MkdirAll(trashDir, 0o700); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		name := r.PathValue("name")
		var suffix [6]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		id := fmt.Sprintf("%s-%s-%x", name, time.Now().UTC().Format("20060102T150405.000000000Z"), suffix)
		if err := os.Rename(path, filepath.Join(trashDir, id)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(filepath.Join(trashDir, id+".json"), []byte(fmt.Sprintf("{\"original\":%q}", name)), 0o600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api/v1/trash", func(w http.ResponseWriter, _ *http.Request) {
		trashDir := filepath.Join(configRoot, ".gickup-trash")
		entries, err := os.ReadDir(trashDir)
		if err != nil {
			if os.IsNotExist(err) {
				writeWebUIJSON(w, map[string]interface{}{"trash": []interface{}{}})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		trash := make([]map[string]string, 0)
		for _, entry := range entries {
			if entry.IsDir() || strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			metadata, err := os.ReadFile(filepath.Join(trashDir, entry.Name()+".json"))
			if err != nil {
				continue
			}
			var item struct {
				Original string `json:"original"`
			}
			if json.Unmarshal(metadata, &item) != nil || !webUIConfigName(item.Original) {
				continue
			}
			trash = append(trash, map[string]string{"id": entry.Name(), "original": item.Original})
		}
		writeWebUIJSON(w, map[string]interface{}{"trash": trash})
	})
	mux.HandleFunc("POST /api/v1/trash/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		trashDir := filepath.Join(configRoot, ".gickup-trash")
		id := r.PathValue("id")
		if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `/\\`) {
			http.Error(w, "invalid trash entry", http.StatusBadRequest)
			return
		}
		metadata, err := os.ReadFile(filepath.Join(trashDir, id+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var item struct {
			Original string `json:"original"`
		}
		if json.Unmarshal(metadata, &item) != nil || !webUIPrepareTarget(w, configRoot, item.Original) {
			return
		}
		if err := os.Rename(filepath.Join(trashDir, id), filepath.Join(configRoot, item.Original)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Remove(filepath.Join(trashDir, id+".json")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("DELETE /api/v1/trash/{id}", func(w http.ResponseWriter, r *http.Request) {
		trashDir := filepath.Join(configRoot, ".gickup-trash")
		id := r.PathValue("id")
		if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `/\\`) {
			http.Error(w, "invalid trash entry", http.StatusBadRequest)
			return
		}
		if err := os.Remove(filepath.Join(trashDir, id)); err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Remove(filepath.Join(trashDir, id+".json")); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/configs/{name}/review", func(w http.ResponseWriter, r *http.Request) {
		path, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var request webUIEditRequest
		if !webUIDecodeRequest(w, r, &request) {
			return
		}
		sum := sha256.Sum256(raw)
		if request.Version == "" || request.Version != hex.EncodeToString(sum[:]) {
			http.Error(w, "configuration changed", http.StatusConflict)
			return
		}
		draft, err := webUIEditYAML(raw, request.Operations)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		draft = webUIPreserveLineEndings(raw, draft)
		errors := webUIValidateDraft(draft, schema)
		valid := len(errors) == 0
		writeWebUIJSON(w, map[string]interface{}{
			"yaml":   webUIMaskSensitiveYAML(draft),
			"diff":   webUIUnifiedDiff(r.PathValue("name"), []byte(webUIMaskSensitiveYAML(raw)), []byte(webUIMaskSensitiveYAML(draft))),
			"valid":  valid,
			"errors": errors,
		})
	})
	mux.HandleFunc("GET /api/v1/configs/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		path, ok := webUIExistingConfig(w, configRoot, name)
		if !ok {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(raw) > webUIMaxConfigSize {
			http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
			return
		}
		if !utf8.Valid(raw) {
			http.Error(w, "configuration must be UTF-8", http.StatusBadRequest)
			return
		}
		documents, warnings, err := webUIParseDocuments(raw, schema)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		maskedDocuments := webUIMaskSensitiveValue(documents)
		var maskedData map[string]interface{}
		if len(maskedDocuments) != 0 {
			maskedData = maskedDocuments[0]
		}
		sum := sha256.Sum256(raw)
		writeWebUIJSON(w, map[string]interface{}{
			"data":      maskedData,
			"documents": maskedDocuments,
			"warnings":  warnings,
			"schema":    schema,
			"uiSchema":  map[string]interface{}{},
			"yaml":      map[string]interface{}{"file": name, "size": len(raw), "source": webUIMaskSensitiveYAML(raw)},
			"version":   hex.EncodeToString(sum[:]),
		})
	})
	mux.HandleFunc("PATCH /api/v1/configs/{name}", func(w http.ResponseWriter, r *http.Request) {
		path, ok := webUIExistingConfig(w, configRoot, r.PathValue("name"))
		if !ok {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var request webUIEditRequest
		if !webUIDecodeRequest(w, r, &request) {
			return
		}
		if request.Version == "" {
			http.Error(w, "version is required", http.StatusBadRequest)
			return
		}
		if !request.Confirmed {
			http.Error(w, "save confirmation is required", http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256(raw)
		if request.Version != hex.EncodeToString(sum[:]) {
			w.WriteHeader(http.StatusConflict)
			writeWebUIJSON(w, map[string]interface{}{
				"error":          "configuration changed",
				"currentVersion": hex.EncodeToString(sum[:]),
			})
			return
		}
		output, err := webUIEditYAML(raw, request.Operations)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors := webUIValidateDraft(output, schema); len(errors) != 0 {
			http.Error(w, strings.Join(errors, "\n"), http.StatusUnprocessableEntity)
			return
		}
		output = webUIPreserveLineEndings(raw, output)
		info, err := os.Stat(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(path+".bak", raw, info.Mode().Perm()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		temp, err := os.CreateTemp(configRoot, ".gickup-webui-*")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tempName := temp.Name()
		defer os.Remove(tempName)
		if err := temp.Chmod(info.Mode().Perm()); err != nil {
			temp.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := temp.Write(output); err != nil {
			temp.Close()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := temp.Close(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tempName, path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	dist, err := fs.Sub(webUIAssets, "webui/dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded WebUI assets: %w", err)
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read embedded WebUI index: %w", err)
	}
	fileServer := http.FileServer(http.FS(dist))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		assetPath := strings.TrimPrefix(r.URL.Path, "/")
		if assetPath != "" {
			if info, statErr := fs.Stat(dist, assetPath); statErr == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			} else if statErr != nil && !os.IsNotExist(statErr) {
				http.Error(w, statErr.Error(), http.StatusInternalServerError)
				return
			}
			if filepath.Ext(assetPath) != "" {
				http.NotFound(w, r)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
	return mux, nil
}

func webUIConfigName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || filepath.IsAbs(name) || name != filepath.Base(name) || strings.ContainsAny(name, `/\\`) {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yml" || ext == ".yaml"
}

func webUIExistingConfig(w http.ResponseWriter, root, name string) (string, bool) {
	if !webUIConfigName(name) {
		http.Error(w, "invalid configuration name", http.StatusBadRequest)
		return "", false
	}
	path := filepath.Join(root, name)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.Error(w, "configuration outside workspace", http.StatusBadRequest)
		return "", false
	}
	info, err := os.Lstat(path)
	if err != nil {
		http.NotFound(w, nil)
		return "", false
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		http.Error(w, "unsafe configuration", http.StatusBadRequest)
		return "", false
	}
	return path, true
}

func webUIPrepareTarget(w http.ResponseWriter, root, name string) bool {
	if !webUIConfigName(name) {
		http.Error(w, "invalid configuration name", http.StatusBadRequest)
		return false
	}
	rel, err := filepath.Rel(root, filepath.Join(root, name))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.Error(w, "configuration outside workspace", http.StatusBadRequest)
		return false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), name) {
			http.Error(w, "configuration already exists", http.StatusConflict)
			return false
		}
	}
	return true
}

func webUIDecodeRequest(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, webUIMaxConfigSize)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return false
	}
	if !utf8.Valid(raw) {
		http.Error(w, "request must be UTF-8", http.StatusBadRequest)
		return false
	}
	if err := json.Unmarshal(raw, target); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	return true
}

func webUIYAMLDocuments(raw []byte) (int, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	count := 0
	for {
		var value interface{}
		err := dec.Decode(&value)
		if err == io.EOF {
			return count, true
		}
		if err != nil {
			return count, false
		}
		if value != nil {
			count++
		}
	}
}

func webUILF(raw []byte) []byte { return bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")) }

func webUIMaskSensitiveValue(documents []map[string]interface{}) []map[string]interface{} {
	raw, _ := json.Marshal(documents)
	var masked []map[string]interface{}
	_ = json.Unmarshal(raw, &masked)
	var walk func(interface{})
	walk = func(value interface{}) {
		switch current := value.(type) {
		case map[string]interface{}:
			for key, child := range current {
				lower := strings.ToLower(key)
				if lower == "token" || lower == "password" || lower == "secret" || lower == "clientsecret" || lower == "secretkey" || lower == "accesskey" {
					if _, ok := child.(string); ok {
						current[key] = "********"
						continue
					}
				}
				walk(child)
			}
		case []interface{}:
			for _, child := range current {
				walk(child)
			}
		}
	}
	for _, document := range masked {
		walk(document)
	}
	return masked
}

func webUIMaskSensitiveYAML(raw []byte) string {
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		content := strings.TrimSpace(line)
		if strings.HasPrefix(content, "- ") {
			content = strings.TrimSpace(strings.TrimPrefix(content, "- "))
		}
		colon := strings.IndexByte(content, ':')
		if colon < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(content[:colon]))
		if key == "token" || key == "password" || key == "secret" || key == "clientsecret" || key == "secretkey" || key == "accesskey" {
			value := strings.Index(line, ":")
			lines[i] = line[:value+1] + " ********"
		}
	}
	return strings.Join(lines, "\n")
}

func webUIPreserveLineEndings(original, output []byte) []byte {
	output = webUILF(output)
	if bytes.Contains(original, []byte("\r\n")) {
		return bytes.ReplaceAll(output, []byte("\n"), []byte("\r\n"))
	}
	return output
}

func webUIUnifiedDiff(name string, original, draft []byte) string {
	originalLines := strings.Split(strings.TrimSuffix(string(original), "\n"), "\n")
	draftLines := strings.Split(strings.TrimSuffix(string(draft), "\n"), "\n")
	var diff strings.Builder
	fmt.Fprintf(&diff, "--- %s\n+++ %s (draft)\n", name, name)
	diff.WriteString("@@\n")
	for _, line := range originalLines {
		diff.WriteString("-")
		diff.WriteString(line)
		diff.WriteString("\n")
	}
	for _, line := range draftLines {
		diff.WriteString("+")
		diff.WriteString(line)
		diff.WriteString("\n")
	}
	return diff.String()
}

func writeWebUIJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
