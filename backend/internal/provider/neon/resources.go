package neon

import (
	"context"
	"time"
)

// Databases uses the documented full-list branch endpoint. It does not accept
// a database name as global identity or request any SQL connection material.
func (c *Client) Databases(ctx context.Context, key, projectID, branchID string) (*DatabaseInventory, error) {
	if !validKey(key) || !validID(projectID) || !validBranchID(branchID) {
		return nil, &Error{Code: "invalid_input"}
	}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	data, err := c.get(ctx, key, "/projects/"+projectID+"/branches/"+branchID+"/databases", nil)
	if err != nil {
		return nil, err
	}
	databases, err := resourceDatabases(data, projectID, branchID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, &Error{Code: "canceled"}
	}
	return &DatabaseInventory{ProjectID: projectID, BranchID: branchID, Databases: databases, ObservedAt: time.Now().UTC(), Complete: true}, nil
}

// Endpoints lists compute metadata for one explicit project. Reported lifecycle
// state is not a SQL probe, application health result or deployment environment.
func (c *Client) Endpoints(ctx context.Context, key, projectID string) (*EndpointInventory, error) {
	if !validKey(key) || !validID(projectID) {
		return nil, &Error{Code: "invalid_input"}
	}
	ctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
	defer cancel()
	data, err := c.get(ctx, key, "/projects/"+projectID+"/endpoints", nil)
	if err != nil {
		return nil, err
	}
	endpoints, err := resourceEndpoints(data, projectID)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, &Error{Code: "canceled"}
	}
	return &EndpointInventory{ProjectID: projectID, Endpoints: endpoints, ObservedAt: time.Now().UTC(), Complete: true}, nil
}

var _ ResourceMetadataAPI = (*Client)(nil)
