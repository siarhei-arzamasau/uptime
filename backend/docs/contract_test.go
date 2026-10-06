package docs_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/swaggo/swag/v2/gen"
	"uptime-app/backend/internal/openapi"
)

func TestContractMatchesAnnotations(t *testing.T) {
	out := t.TempDir()
	err := gen.New().Build(&gen.Config{
		SearchDir:           "../cmd/api,../internal/auth,../internal/profile,../internal/monitor,../internal/httpx,../internal/store",
		MainAPIFile:         "main.go",
		OutputDir:           out,
		OutputTypes:         []string{"json", "yaml"},
		PropNamingStrategy:  "camelcase",
		ParseInternal:       true,
		ParseGoList:         true,
		ParseDepth:          100,
		RequiredByDefault:   true,
		GenerateOpenAPI3Doc: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := openapi.NormalizeNullable(out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"swagger.json", "swagger.yaml"} {
		committed, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		generated, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(committed, generated) {
			t.Errorf("%s is stale; run go generate ./cmd/api from backend", name)
		}
	}
}

func TestContractCoversRegisteredRoutesAndSecurity(t *testing.T) {
	data, err := os.ReadFile("swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Security []map[string][]string `json:"security"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type   string `json:"type"`
				Scheme string `json:"scheme"`
				In     string `json:"in"`
				Name   string `json:"name"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.OpenAPI != "3.1.0" {
		t.Fatalf("expected OpenAPI 3.1.0, got %q", contract.OpenAPI)
	}
	bearer := contract.Components.SecuritySchemes["BearerAuth"]
	if bearer.Type != "http" || bearer.Scheme != "bearer" {
		t.Error("BearerAuth must describe an HTTP bearer token")
	}
	cookie := contract.Components.SecuritySchemes["RefreshCookie"]
	if cookie.Type != "apiKey" || cookie.In != "cookie" || cookie.Name != "refresh_token" {
		t.Error("RefreshCookie must describe the refresh_token cookie")
	}
	documented := make(map[string]bool)
	for path, methods := range contract.Paths {
		for method, operation := range methods {
			documented[strings.ToUpper(method)+" /api/v1"+path] = true
			for _, requirement := range operation.Security {
				for name := range requirement {
					if _, exists := contract.Components.SecuritySchemes[name]; !exists {
						t.Errorf("%s %s references undefined security scheme %s", method, path, name)
					}
				}
			}
		}
	}
	err = filepath.WalkDir("../internal", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			route, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !documented[route] {
				t.Errorf("registered route %s in %s has no OpenAPI operation", route, path)
			}
			delete(documented, route)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for route := range documented {
		t.Errorf("OpenAPI operation %s has no registered controller route", route)
	}
}

func TestMonitoringNullability(t *testing.T) {
	data, err := os.ReadFile("swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Components struct {
			Schemas map[string]struct{ Properties map[string]struct{ Type any } }
		}
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for name, fields := range map[string][]string{
		"store.MonitorStatus":  {"last_started_at", "last_finished_at", "last_success", "http_status"},
		"store.MonitorHistory": {"availability"},
		"store.HistoryBucket":  {"availability"},
	} {
		for _, field := range fields {
			types, ok := document.Components.Schemas[name].Properties[field].Type.([]any)
			if !ok || len(types) != 2 || types[1] != "null" {
				t.Errorf("%s.%s must accept null", name, field)
			}
		}
	}
}
