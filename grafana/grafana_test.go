package grafana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New(Options{BaseURL: "http://grafana:3000"})
	if c.baseURL != "http://grafana:3000" {
		t.Fatalf("baseURL = %s", c.baseURL)
	}
	if c.orgID != 1 {
		t.Fatalf("default orgID = %d", c.orgID)
	}
	if c.http.Timeout != 30*time.Second {
		t.Fatalf("default timeout = %v", c.http.Timeout)
	}

	c2 := New(Options{BaseURL: "http://g/", APIKey: "tok", OrgID: 5, Timeout: 10 * time.Second})
	if c2.baseURL != "http://g" {
		t.Fatalf("baseURL should be trimmed: %s", c2.baseURL)
	}
	if c2.orgID != 5 || c2.apiKey != "tok" {
		t.Fatalf("opts not applied: %+v", c2)
	}
}

func TestRenderPanelValidation(t *testing.T) {
	// 空 baseURL
	c := New(Options{})
	if _, err := c.RenderPanel(context.Background(), PanelOptions{DashboardUID: "u", PanelID: 1}); err == nil {
		t.Fatal("expected error for empty base url")
	}

	// 缺少 dashboard/panel
	c2 := New(Options{BaseURL: "http://g"})
	if _, err := c2.RenderPanel(context.Background(), PanelOptions{}); err == nil {
		t.Fatal("expected error for missing dashboard/panel")
	}
	if _, err := c2.RenderPanel(context.Background(), PanelOptions{DashboardUID: "u", PanelID: 0}); err == nil {
		t.Fatal("expected error for invalid panel id")
	}
}

func TestRenderPanelOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/render/d-solo/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNGDATA"))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	data, err := c.RenderPanel(context.Background(), PanelOptions{
		DashboardUID: "uid",
		PanelID:      1,
		From:         time.Now().Add(-time.Hour),
		To:           time.Now(),
		Vars:         map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("RenderPanel err: %v", err)
	}
	if !strings.Contains(string(data), "PNGDATA") {
		t.Fatalf("unexpected data: %s", data)
	}
}

func TestRenderPanelWrongContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>no renderer</html>"))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	if _, err := c.RenderPanel(context.Background(), PanelOptions{DashboardUID: "u", PanelID: 1}); err == nil {
		t.Fatal("expected error for non-image content-type")
	}
}

func ExampleNew() {
	c := New(Options{
		BaseURL: "http://grafana:3000",
		APIKey:  "service-token",
		OrgID:   1,
	})
	_, _ = c.RenderPanel(context.Background(), PanelOptions{
		DashboardUID: "demo",
		PanelID:      2,
		Width:        1000,
		Height:       500,
	})
}
