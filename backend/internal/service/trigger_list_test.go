package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store/memory"
)

func TestTriggerListIntentsIncludesReviewFiltersAppAndBounds(t *testing.T) {
	ctx := context.Background()
	st := memory.NewTriggerStore()
	svc := &TriggerService{Store: st}
	rule, event := triggerFixture()
	if err := svc.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	review := *rule
	review.ID = "review"
	review.Version = 0
	review.AppID = "other-app"
	review.Approval = "review"
	if err := svc.SaveRule(ctx, &review); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 51; n++ {
		event.DeliveryID = fmt.Sprintf("delivery-%d", n)
		event.CommitSHA = fmt.Sprintf("%040x", n+1)
		if _, _, err := svc.AcceptVerified(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.ListIntents(ctx, "", 1000)
	if err != nil || len(all) != 100 {
		t.Fatalf("bounded list: %d %v", len(all), err)
	}
	other, err := st.ListIntents(ctx, "other-app", 2)
	if err != nil || len(other) != 2 {
		t.Fatalf("filtered list: %d %v", len(other), err)
	}
	if other[0].Status != model.TriggerAwaitingReview || other[0].Rule.AppID != "other-app" || other[0].CreatedAt.Before(other[1].CreatedAt) {
		t.Fatalf("wrong recent review list: %+v", other)
	}
	id := other[0].ID
	other[0].Rule.AppID = "mutated"
	original, err := st.GetIntent(ctx, id)
	if err != nil || original.Rule.AppID != "other-app" {
		t.Fatal("list exposed mutable store data")
	}
	empty, err := st.ListIntents(ctx, "missing", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty list: %v %v", empty, err)
	}
}
