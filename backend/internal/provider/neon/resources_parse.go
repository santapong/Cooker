package neon

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Checked 26 October 2026 against Neon's maintained OpenAPI:
// https://neon.com/api_spec/release/v2.json (Database, DatabasesResponse,
// Endpoint, EndpointsResponse, EndpointType, EndpointState).
// These two list operations document no pagination. Unknown continuation or
// partial-result markers cannot be followed or interpreted as complete.
func resourceRecords(data []byte, name string) ([]json.RawMessage, error) {
	fields, err := object(data)
	if err != nil {
		return nil, err
	}
	for key := range fields {
		switch strings.ToLower(key) {
		case "pagination", "cursor", "next", "next_cursor", "nextcursor", "next_page", "nextpage", "next_token", "continuation_token", "offset", "limit", "total", "total_count", "has_more", "hasmore", "truncated", "incomplete", "complete", "unavailable", "unavailable_database_ids", "unavailable_endpoint_ids", "unavailable_databases", "unavailable_endpoints", "error", "errors":
			return nil, &Error{Code: "incomplete_coverage"}
		}
	}
	value, ok := fields[name]
	var rows []json.RawMessage
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &rows) != nil || rows == nil {
		return nil, &Error{Code: "invalid_response"}
	}
	// Existing records() is for 100-row pages. These full-list endpoints need
	// their own 1000-item bound, with no silent truncation or guessed next page.
	if len(rows) > maxItems {
		return nil, &Error{Code: "inventory_limit"}
	}
	return rows, nil
}

func resourceDatabases(data []byte, projectID, branchID string) ([]Database, error) {
	rows, err := resourceRecords(data, "databases")
	if err != nil {
		return nil, err
	}
	out := []Database{}
	seenIDs, seenNames := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		fields, err := object(row)
		if err != nil {
			return nil, err
		}
		var database Database
		value, ok := fields["id"]
		var id int64
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &id) != nil {
			return nil, &Error{Code: "invalid_response"}
		}
		database.ID = strconv.FormatInt(id, 10)
		if database.BranchID, err = stringField(fields, "branch_id", true); err != nil {
			return nil, err
		}
		if !validBranchID(database.BranchID) {
			return nil, &Error{Code: "invalid_response"}
		}
		if database.BranchID != branchID {
			return nil, &Error{Code: "scope_mismatch"}
		}
		// Database has no project_id in the current schema; the explicit request
		// path supplies its project scope. Reject any contradictory extension.
		if _, present := fields["project_id"]; present {
			project, err := stringField(fields, "project_id", true)
			if err != nil || !validID(project) {
				return nil, &Error{Code: "invalid_response"}
			}
			if project != projectID {
				return nil, &Error{Code: "scope_mismatch"}
			}
		}
		database.ProjectID = projectID
		if database.Name, err = stringField(fields, "name", true); err != nil {
			return nil, err
		}
		if _, err := safeName(database.Name); err != nil {
			return nil, err
		}
		// A quoted database name can contain significant spaces. Validate its
		// bounds without trimming or turning it into a cross-project identity.
		if seenIDs[database.ID] || seenNames[database.Name] {
			return nil, &Error{Code: "incomplete_coverage"}
		}
		seenIDs[database.ID], seenNames[database.Name] = true, true
		out = append(out, database)
	}
	return out, nil
}

func resourceEndpoints(data []byte, projectID string) ([]Endpoint, error) {
	rows, err := resourceRecords(data, "endpoints")
	if err != nil {
		return nil, err
	}
	out, seen := []Endpoint{}, map[string]bool{}
	for _, row := range rows {
		fields, err := object(row)
		if err != nil {
			return nil, err
		}
		var endpoint Endpoint
		if endpoint.ID, err = stringField(fields, "id", true); err != nil {
			return nil, err
		}
		if !validID(endpoint.ID) || !strings.HasPrefix(endpoint.ID, "ep-") || len(endpoint.ID) <= 3 {
			return nil, &Error{Code: "invalid_response"}
		}
		if endpoint.ProjectID, err = stringField(fields, "project_id", true); err != nil {
			return nil, err
		}
		if !validID(endpoint.ProjectID) {
			return nil, &Error{Code: "invalid_response"}
		}
		if endpoint.ProjectID != projectID {
			return nil, &Error{Code: "scope_mismatch"}
		}
		if endpoint.BranchID, err = stringField(fields, "branch_id", true); err != nil {
			return nil, err
		}
		if !validBranchID(endpoint.BranchID) {
			return nil, &Error{Code: "invalid_response"}
		}
		typeValue, err := stringField(fields, "type", true)
		if err != nil {
			return nil, err
		}
		endpoint.Type = "unknown"
		if typeValue == "read_only" || typeValue == "read_write" {
			endpoint.Type = typeValue
		}
		stateValue, err := stringField(fields, "current_state", true)
		if err != nil {
			return nil, err
		}
		endpoint.CurrentState = "unknown"
		if stateValue == "init" || stateValue == "active" || stateValue == "idle" {
			endpoint.CurrentState = stateValue
		}
		release, err := stringField(fields, "compute_release_version", false)
		if err != nil {
			return nil, err
		}
		if safeComputeRelease(release) {
			endpoint.ComputeReleaseVersion = release
		}
		if seen[endpoint.ID] {
			return nil, &Error{Code: "incomplete_coverage"}
		}
		seen[endpoint.ID] = true
		out = append(out, endpoint)
	}
	return out, nil
}

func safeComputeRelease(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '-' || ch == '+') {
			return false
		}
	}
	return true
}
