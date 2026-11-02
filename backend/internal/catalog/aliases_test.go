package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/santapong/cooker/internal/provider/vercel"
)

func TestProductionAliasesUseExactIndependentObservation(t *testing.T) {
	s, v := fixture()
	d := v.Projects[0].Details.ProductionDeployment
	at := fixtureTime.Add(-time.Hour)
	d.Details.AliasDetails = &vercel.DeploymentAliasDetails{DeploymentID: d.ID, Aliases: []string{"https://custom.example.com", "https://beta.vercel.app"}, ObservedAt: at, Error: "Production aliases could not be refreshed; any previous observation is retained"}
	a := s.Apps(context.Background(), Query{}, false).Items[0]
	prod := a.ProductionDeployment
	if prod.AliasFreshness == nil || prod.AliasFreshness.State != "stale" || !prod.AliasFreshness.ObservedAt.Equal(at) || len(prod.ProductionURLs) != 2 {
		t.Fatal("independent retained alias observation lost", prod)
	}
	if !prod.MetadataFreshness.ObservedAt.Equal(fixtureTime) || prod.MetadataFreshness.Error != "" || !a.SourceUnavailable || a.Freshness.State != "stale" {
		t.Fatal("alias failure falsely refreshed or disappeared", a)
	}
	d.Details.AliasDetails = &vercel.DeploymentAliasDetails{DeploymentID: d.ID, Aliases: []string{}, ObservedAt: fixtureTime}
	prod = s.Apps(context.Background(), Query{}, false).Items[0].ProductionDeployment
	if len(prod.ProductionURLs) != 0 || prod.AliasFreshness.State != "up_to_date" {
		t.Fatal("explicit empty authoritative response reused old aliases", prod)
	}
	d.Details.AliasDetails = &vercel.DeploymentAliasDetails{DeploymentID: "dpl_other", Aliases: []string{"https://other.example.com"}, ObservedAt: fixtureTime}
	prod = s.Apps(context.Background(), Query{}, false).Items[0].ProductionDeployment
	if len(prod.ProductionURLs) != 0 || prod.AliasFreshness.State != "unavailable" {
		t.Fatal("unrelated deployment aliases accepted", prod)
	}
}

func TestOverlappingConnectionsMergeAliasClockSeparately(t *testing.T) {
	s, v := fixture()
	prod := v.Projects[0].Details.ProductionDeployment
	prod.Details.AliasDetails = &vercel.DeploymentAliasDetails{DeploymentID: prod.ID, Aliases: []string{"https://old.example.com"}, ObservedAt: fixtureTime}
	newClock := fixtureTime.Add(time.Minute)
	other := *v.Connections[0]
	other.ID = "conn_overlap"
	other.LastSuccessAt = &newClock
	v.Connections = append(v.Connections, &other)
	p := v.Projects[0]
	p.ConnectionID = other.ID
	pd := *p.Details
	p.Details = &pd
	d := *prod
	detail := *prod.Details
	detail.ObservedAt = fixtureTime.Add(-time.Minute)
	detail.AliasDetails = &vercel.DeploymentAliasDetails{DeploymentID: prod.ID, Aliases: []string{"https://new.example.com"}, ObservedAt: newClock}
	d.Details = &detail
	pd.ProductionDeployment = &d
	// Production is outside this connection's recent history; its nested
	// observation still participates in exact account/project/deployment merging.
	v.Projects = append(v.Projects, p)
	a := s.Apps(context.Background(), Query{}, false).Items[0]
	if a.ProductionDeployment == nil || !a.ProductionDeployment.MetadataFreshness.ObservedAt.Equal(fixtureTime) || !a.ProductionDeployment.AliasFreshness.ObservedAt.Equal(newClock) || len(a.ProductionDeployment.ProductionURLs) != 1 || a.ProductionDeployment.ProductionURLs[0] != "https://new.example.com" {
		t.Fatal("deployment clock discarded independently fresher aliases", a)
	}
	if prod.Details.AliasDetails.Aliases[0] != "https://old.example.com" {
		t.Fatal("catalog read mutated provider snapshot")
	}
	other.ProjectIDs = nil
	a = s.Apps(context.Background(), Query{}, false).Items[0]
	if a.ProductionDeployment.ProductionURLs[0] != "https://old.example.com" || !a.ProductionDeployment.AliasFreshness.ObservedAt.Equal(fixtureTime) {
		t.Fatal("deselected connection still enriched visible production", a)
	}
}

func TestHistoricalAndPreviewAliasesAreNotProductionDestinations(t *testing.T) {
	s, v := fixture()
	for i := range v.Deployments {
		d := &v.Deployments[i]
		d.Details.AliasDetails = &vercel.DeploymentAliasDetails{DeploymentID: d.ID, Aliases: []string{"https://release.example.com"}, ObservedAt: fixtureTime}
	}
	detail, err := s.App(context.Background(), s.Apps(context.Background(), Query{}, false).Items[0].ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range detail.Deployments {
		if d.ID == "dpl_preview" && (len(d.ProductionURLs) != 0 || d.AliasFreshness != nil) {
			t.Fatal("preview presented as a current production destination", d)
		}
	}
}
