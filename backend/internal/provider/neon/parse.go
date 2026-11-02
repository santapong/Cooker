package neon

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Wire contracts checked 25 October 2026 against Neon's maintained OpenAPI:
// https://neon.com/api_spec/release/v2.json (ProjectListItem, Branch,
// ProjectsResponse, Pagination, CursorPagination). Unknown metadata is ignored.
func object(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, &Error{Code: "invalid_response"}
	}
	out, seen := map[string]json.RawMessage{}, map[string]bool{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, &Error{Code: "invalid_response"}
		}
		key, ok := keyToken.(string)
		if !ok || seen[strings.ToLower(key)] {
			return nil, &Error{Code: "invalid_response"}
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, &Error{Code: "invalid_response"}
		}
		seen[strings.ToLower(key)], out[key] = true, value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, &Error{Code: "invalid_response"}
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, &Error{Code: "invalid_response"}
	}
	return out, nil
}

func stringField(fields map[string]json.RawMessage, name string, required bool) (string, error) {
	value, ok := fields[name]
	if !ok && !required {
		return "", nil
	}
	var text string
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &text) != nil || (required && text == "") {
		return "", &Error{Code: "invalid_response"}
	}
	return text, nil
}

func timeField(fields map[string]json.RawMessage, name string) (time.Time, error) {
	value, err := stringField(fields, name, true)
	if err != nil {
		return time.Time{}, err
	}
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || timestamp.IsZero() || timestamp.Year() < 1 || timestamp.Year() > 9999 {
		return time.Time{}, &Error{Code: "invalid_response"}
	}
	return timestamp.UTC(), nil
}

func records(fields map[string]json.RawMessage, name string) ([]json.RawMessage, error) {
	value, ok := fields[name]
	var rows []json.RawMessage
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &rows) != nil || rows == nil {
		return nil, &Error{Code: "invalid_response"}
	}
	if len(rows) > pageSize {
		return nil, &Error{Code: "inventory_limit"}
	}
	return rows, nil
}

func projectPage(data []byte, organizationID string) ([]Project, string, error) {
	fields, err := object(data)
	if err != nil {
		return nil, "", err
	}
	// The old reference called this unavailable; its shape is not this contract.
	if _, legacy := fields["unavailable"]; legacy {
		return nil, "", &Error{Code: "incomplete_coverage"}
	}
	if value, ok := fields["unavailable_project_ids"]; ok {
		var ids []string
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &ids) != nil || ids == nil {
			return nil, "", &Error{Code: "invalid_response"}
		}
		if len(ids) > 0 {
			return nil, "", &Error{Code: "incomplete_coverage"}
		}
	}
	rows, err := records(fields, "projects")
	if err != nil {
		return nil, "", err
	}
	out := []Project{}
	for _, row := range rows {
		fields, err := object(row)
		if err != nil {
			return nil, "", err
		}
		var p Project
		if p.ID, err = stringField(fields, "id", true); err != nil {
			return nil, "", err
		}
		if !validID(p.ID) {
			return nil, "", &Error{Code: "invalid_response"}
		}
		owner, err := stringField(fields, "owner_id", true)
		if err != nil || !validID(owner) {
			return nil, "", &Error{Code: "invalid_response"}
		}
		// Current schema defines owner_id as the owning organization ID. Do not
		// infer an alias when legacy owner_id and optional org_id contradict it.
		if owner != organizationID {
			return nil, "", &Error{Code: "scope_mismatch"}
		}
		p.OrganizationID = owner
		if _, present := fields["org_id"]; present {
			org, err := stringField(fields, "org_id", true)
			if err != nil || !validID(org) {
				return nil, "", &Error{Code: "invalid_response"}
			}
			if org != owner {
				return nil, "", &Error{Code: "scope_mismatch"}
			}
		}
		name, err := stringField(fields, "name", true)
		if err != nil {
			return nil, "", err
		}
		if p.Name, err = safeName(name); err != nil {
			return nil, "", err
		}
		if p.RegionID, err = stringField(fields, "region_id", true); err != nil {
			return nil, "", err
		}
		if !validID(p.RegionID) {
			return nil, "", &Error{Code: "invalid_response"}
		}
		if p.CreatedAt, err = timeField(fields, "created_at"); err != nil {
			return nil, "", err
		}
		if p.UpdatedAt, err = timeField(fields, "updated_at"); err != nil {
			return nil, "", err
		}
		out = append(out, p)
	}
	cursor := ""
	if value, ok := fields["pagination"]; ok {
		pagination, err := object(value)
		if err != nil {
			return nil, "", err
		}
		if _, wrong := pagination["next"]; wrong {
			return nil, "", &Error{Code: "invalid_response"}
		}
		if cursor, err = stringField(pagination, "cursor", true); err != nil {
			return nil, "", err
		}
		if !validCursor(cursor) {
			return nil, "", &Error{Code: "invalid_response"}
		}
	} else if len(rows) == pageSize {
		return nil, "", &Error{Code: "incomplete_coverage"}
	}
	return out, cursor, nil
}

func branchPage(data []byte, projectID string) ([]Branch, string, error) {
	fields, err := object(data)
	if err != nil {
		return nil, "", err
	}
	rows, err := records(fields, "branches")
	if err != nil {
		return nil, "", err
	}
	out := []Branch{}
	for _, row := range rows {
		fields, err := object(row)
		if err != nil {
			return nil, "", err
		}
		var b Branch
		if b.ID, err = stringField(fields, "id", true); err != nil {
			return nil, "", err
		}
		if !validBranchID(b.ID) {
			return nil, "", &Error{Code: "invalid_response"}
		}
		if b.ProjectID, err = stringField(fields, "project_id", true); err != nil {
			return nil, "", err
		}
		if b.ProjectID != projectID {
			return nil, "", &Error{Code: "scope_mismatch"}
		}
		if _, present := fields["parent_id"]; present {
			if b.ParentID, err = stringField(fields, "parent_id", true); err != nil {
				return nil, "", err
			}
			if !validBranchID(b.ParentID) || b.ParentID == b.ID {
				return nil, "", &Error{Code: "invalid_response"}
			}
		}
		name, err := stringField(fields, "name", true)
		if err != nil {
			return nil, "", err
		}
		if b.Name, err = safeName(name); err != nil {
			return nil, "", err
		}
		if b.CreatedAt, err = timeField(fields, "created_at"); err != nil {
			return nil, "", err
		}
		if b.UpdatedAt, err = timeField(fields, "updated_at"); err != nil {
			return nil, "", err
		}
		out = append(out, b)
	}
	next := ""
	if value, ok := fields["pagination"]; ok {
		pagination, err := object(value)
		if err != nil {
			return nil, "", err
		}
		if _, wrong := pagination["cursor"]; wrong {
			return nil, "", &Error{Code: "invalid_response"}
		}
		if _, present := pagination["next"]; present {
			if next, err = stringField(pagination, "next", true); err != nil {
				return nil, "", err
			}
			if !validCursor(next) {
				return nil, "", &Error{Code: "invalid_response"}
			}
		}
		for key, expected := range map[string]string{"sort_by": "created_at", "sort_order": "asc"} {
			if _, present := pagination[key]; present {
				reported, err := stringField(pagination, key, true)
				if err != nil || reported != expected {
					return nil, "", &Error{Code: "invalid_response"}
				}
			}
		}
	}
	return out, next, nil
}
