package memory

import (
	"context"
	"sort"

	"github.com/santapong/cooker/internal/model"
	"github.com/santapong/cooker/internal/store"
)

func (t *TriggerStore) ListTriggerHistory(_ context.Context, query store.TriggerHistoryQuery) (*model.TriggerHistoryPage, error) {
	q, err := query.Normalize()
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	page := store.NewTriggerHistoryPage(q)
	cutover := t.auditStartedAt
	page.JournalStartedAt = &cutover
	items := []model.TriggerHistoryRow{}
	for _, d := range t.deliveries {
		decisions := append([]model.TriggerDecision(nil), d.Decisions...)
		journal, journaled := t.decisionJournal[d.ID]
		if journaled {
			decisions = []model.TriggerDecision{}
			for _, j := range journal {
				decisions = append(decisions, model.TriggerDecision{RuleID: j.RuleID, RuleVersion: j.RuleVersion, AppID: j.AppID, EnvironmentID: j.EnvironmentID, TargetKind: j.TargetKind, TargetID: j.TargetID, IntentID: j.IntentID, Status: j.Outcome})
			}
		}
		if len(decisions) == 0 && d.Archived && !journaled {
			ids := []string{}
			for _, i := range t.intents {
				if i.DeliveryID == d.ID {
					ids = append(ids, i.ID)
				}
			}
			sort.Strings(ids)
			for _, id := range ids {
				i := t.intents[id]
				decisions = append(decisions, model.TriggerDecision{IntentID: id, RuleID: i.Rule.ID})
			}
		}
		if len(decisions) == 0 {
			decisions = []model.TriggerDecision{{}}
		}
		for index, decision := range decisions {
			e := d.Event
			row := model.TriggerHistoryRow{DeliveryID: d.ID, ProviderDeliveryID: e.DeliveryID, Provider: e.Source.Provider, ConnectionID: e.Source.ConnectionID, Repository: e.Source.Repository, FullRef: e.FullRef, CommitSHA: e.CommitSHA, Deleted: e.Deleted, RuleID: decision.RuleID, RuleVersion: decision.RuleVersion, StoredStatus: decision.Status, StoredReason: decision.Reason, CreatedAt: d.CreatedAt, UpdatedAt: d.CreatedAt, Archived: d.Archived, DecisionIndex: index + 1}
			row.IntentID = decision.IntentID
			if journaled {
				row.AppID = decision.AppID
				row.EnvironmentID = decision.EnvironmentID
				row.TargetKind = decision.TargetKind
				row.TargetID = decision.TargetID
				row.TargetSource = "decision"
				row.MetadataCoverage = "retained_decision"
				row.StoredReasonCode = journal[index].ReasonCode
			} else {
				row.MetadataCoverage = "legacy"
			}
			if i, ok := t.intents[decision.IntentID]; ok {
				row.IntentID = i.ID
				row.IntentStatus = string(i.Status)
				if !journaled {
					row.AppID = i.Rule.AppID
					row.TargetSource = "intent"
					row.RuleVersion = i.Rule.Version
				}
				row.UpdatedAt = i.UpdatedAt
				if decision.Status != "duplicate" {
					row.StoredReason = i.Reason
				}
				if i.Receipt != nil {
					row.JobID = i.Receipt.JobID
					row.RunID = i.Receipt.RunID
					row.PipelineID = i.Receipt.PipelineID
				}
			} else if r, ok := t.rules[decision.RuleID]; ok && !journaled {
				row.AppID = r.AppID
				row.TargetSource = "current_rule"
			}
			row = store.PresentTriggerHistory(row)
			if store.TriggerHistoryMatches(row, q) {
				items = append(items, row)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		if items[i].DeliveryID != items[j].DeliveryID {
			return items[i].DeliveryID < items[j].DeliveryID
		}
		return items[i].DecisionIndex < items[j].DecisionIndex
	})
	page.Total = len(items)
	if q.Offset >= len(items) {
		return page, nil
	}
	end := q.Offset + q.Limit
	if end > len(items) {
		end = len(items)
	}
	page.Items = items[q.Offset:end]
	return page, nil
}

var _ store.TriggerHistoryStore = (*TriggerStore)(nil)
