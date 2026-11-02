package catalog

import (
	"context"
	"strings"
	"testing"

	"github.com/santapong/cooker/internal/provider/vercel"
)

func TestAggregateApplicationKeepsComponentInventoryUnknown(t *testing.T) {
	s, v := fixture()
	p := &v.Projects[0]
	p.Details.ServicesKnown = false
	d := p.Details.ProductionDeployment.Details
	d.ServicesKnown, d.Services = false, nil
	d.ApplicationObserved = true
	rows := s.Services(context.Background(), Query{Environment: "production"}, false)
	if rows.Total != 1 || rows.Items[0].Name != "Application" || rows.Items[0].Definition != "aggregate" || rows.Items[0].Error == "" || rows.Items[0].Runtime != "" || rows.Items[0].Framework != "" || rows.Items[0].Health != "unknown" || rows.Items[0].Status != "READY" {
		t.Fatal("aggregate observation invented component details", rows)
	}
	d.Error = "Deployment details could not be refreshed"
	rows = s.Services(context.Background(), Query{Environment: "production"}, false)
	if !strings.Contains(rows.Items[0].Error, "did not report its detailed component inventory") || !strings.Contains(rows.Items[0].Error, d.Error) {
		t.Fatal("refresh error hid aggregate inventory limitation", rows)
	}
	d.Error = ""
	// Positive project definitions remain configured, rather than being hidden
	// by the deployment aggregate. Explicit deployment definitions take priority.
	p.Details.ServicesKnown = true
	p.Details.Services = []vercel.ServiceDefinition{{Name: "configured-web"}}
	rows = s.Services(context.Background(), Query{Environment: "production"}, false)
	if rows.Items[0].Definition != "configured" || rows.Items[0].Name != "configured-web" {
		t.Fatal("aggregate hid reported configuration", rows)
	}
	d.ServicesKnown = true
	d.Services = []vercel.ServiceDefinition{{Name: "web"}, {Name: "worker"}}
	rows = s.Services(context.Background(), Query{Environment: "production"}, false)
	if rows.Total != 2 || rows.Items[0].Definition != "deployed" || rows.Items[1].Definition != "deployed" {
		t.Fatal("aggregate hid explicit deployment membership", rows)
	}
	d.ServicesKnown, d.Services, d.ApplicationObserved = false, nil, false
	p.Details.ServicesKnown = false
	rows = s.Services(context.Background(), Query{Environment: "production"}, false)
	if rows.Total != 1 || rows.Items[0].Definition != "unavailable" {
		t.Fatal("reduced response acquired aggregate owner proof", rows)
	}
}
