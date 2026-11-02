package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// StaticProjectAssessment retains predicates only. Provider command/root values
// can contain private information and must never enter this safe projection.
type StaticProjectAssessment struct {
	ProjectID              string    `json:"projectId"`
	OwnerID                string    `json:"ownerId"`
	Name                   string    `json:"name"`
	ProductionDeploymentID string    `json:"productionDeploymentId,omitempty"`
	FrameworkKnown         bool      `json:"frameworkKnown"`
	FrameworkOther         bool      `json:"frameworkOther"`
	BuildCommandKnown      bool      `json:"buildCommandKnown"`
	BuildCommandEmpty      bool      `json:"buildCommandEmpty"`
	InstallCommandKnown    bool      `json:"installCommandKnown"`
	InstallCommandEmpty    bool      `json:"installCommandEmpty"`
	OutputDirectoryKnown   bool      `json:"outputDirectoryKnown"`
	OutputDirectoryDot     bool      `json:"outputDirectoryDot"`
	RootDirectoryKnown     bool      `json:"rootDirectoryKnown"`
	RootDirectoryDefault   bool      `json:"rootDirectoryDefault"`
	GitLinkKnown           bool      `json:"gitLinkKnown"`
	GitLinked              bool      `json:"gitLinked"`
	ServicesKnown          bool      `json:"servicesKnown"`
	HasServices            bool      `json:"hasServices"`
	ObservedAt             time.Time `json:"observedAt"`
	Eligible               bool      `json:"eligible"`
	Reasons                []string  `json:"reasons"`
}

// StaticProjectReadAPI is deliberately separate from inventory and mutation
// interfaces. A successful read does not prove permission to deploy.
type StaticProjectReadAPI interface {
	StaticProject(context.Context, string, string, string) (*StaticProjectAssessment, error)
}

func (c *Client) StaticProject(ctx context.Context, token, teamID, projectID string) (*StaticProjectAssessment, error) {
	if err := validateScope(token, teamID); err != nil {
		return nil, err
	}
	if staticID(projectID, "prj_") == "" {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	data, err := c.staticRequest(ctx, token, http.MethodGet, "/v9/projects/"+projectID, scopeQuery(teamID), nil)
	if err != nil {
		return nil, err
	}
	out, err := staticProjectAssessment(data)
	if err != nil {
		return nil, err
	}
	if out.ProjectID != projectID || (teamID != "" && out.OwnerID != teamID) {
		return nil, invalidResponse()
	}
	return out, nil
}

// Project schema: /docs/rest-api/projects/find-a-project-by-id-or-name, checked
// 25 October 2026. Optional omission is unknown; it is never an empty setting.
func staticProjectAssessment(data []byte) (*StaticProjectAssessment, error) {
	fields, err := staticObject(data)
	if err != nil {
		return nil, invalidResponse()
	}
	var identity struct{ ID, AccountID, Name string }
	if staticDecode(fields, &identity) != nil || staticID(identity.ID, "prj_") == "" || !validProviderID(identity.AccountID) || identity.Name == "" {
		return nil, invalidResponse()
	}
	out := &StaticProjectAssessment{ProjectID: identity.ID, OwnerID: identity.AccountID, Name: displayText(identity.Name, 100), ObservedAt: time.Now().UTC(), Reasons: []string{}}
	if out.Name == "" {
		return nil, invalidResponse()
	}
	if out.FrameworkKnown, out.FrameworkOther, err = staticSetting(fields, "framework", "", true); err != nil {
		return nil, err
	}
	if out.BuildCommandKnown, out.BuildCommandEmpty, err = staticSetting(fields, "buildCommand", "", false); err != nil {
		return nil, err
	}
	if out.InstallCommandKnown, out.InstallCommandEmpty, err = staticSetting(fields, "installCommand", "", false); err != nil {
		return nil, err
	}
	if out.OutputDirectoryKnown, out.OutputDirectoryDot, err = staticSetting(fields, "outputDirectory", ".", false); err != nil {
		return nil, err
	}
	if out.RootDirectoryKnown, out.RootDirectoryDefault, err = staticSetting(fields, "rootDirectory", "", true); err != nil {
		return nil, err
	}
	if value, ok := fields["link"]; ok {
		out.GitLinkKnown = true
		if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if _, err := staticObject(value); err != nil {
				return nil, invalidResponse()
			}
			out.GitLinked = true
		}
	}
	if value, ok := fields["services"]; ok {
		var services []json.RawMessage
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &services) != nil || services == nil {
			return nil, invalidResponse()
		}
		out.ServicesKnown, out.HasServices = true, len(services) > 0
	}
	if value, ok := fields["targets"]; ok {
		var targets map[string]json.RawMessage
		if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if targets, err = staticObject(value); err != nil {
				return nil, invalidResponse()
			}
			if production, ok := targets["production"]; ok && !bytes.Equal(bytes.TrimSpace(production), []byte("null")) {
				fields, err := staticObject(production)
				if err != nil {
					return nil, invalidResponse()
				}
				var target struct{ ID string }
				if staticDecode(fields, &target) != nil || staticID(target.ID, "dpl_") == "" {
					return nil, invalidResponse()
				}
				out.ProductionDeploymentID = target.ID
			}
		}
	}
	out.Reasons = staticEligibilityReasons(out)
	out.Eligible = len(out.Reasons) == 0
	return out, nil
}

func staticSetting(fields map[string]json.RawMessage, key, expected string, nullMatches bool) (bool, bool, error) {
	value, ok := fields[key]
	if !ok {
		return false, false, nil
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nullMatches, nullMatches, nil
	}
	var text string
	if json.Unmarshal(value, &text) != nil {
		return false, false, invalidResponse()
	}
	return true, text == expected, nil
}

func staticEligibilityReasons(a *StaticProjectAssessment) []string {
	reasons := []string{}
	predicates := []struct {
		known, supported  bool
		unknown, rejected string
	}{
		{a.FrameworkKnown, a.FrameworkOther, "framework_unknown", "framework_unsupported"},
		{a.BuildCommandKnown, a.BuildCommandEmpty, "build_command_unknown", "build_command_unsupported"},
		{a.InstallCommandKnown, a.InstallCommandEmpty, "install_command_unknown", "install_command_unsupported"},
		{a.OutputDirectoryKnown, a.OutputDirectoryDot, "output_directory_unknown", "output_directory_unsupported"},
		{a.RootDirectoryKnown, a.RootDirectoryDefault, "root_directory_unknown", "root_directory_unsupported"},
		{a.GitLinkKnown, !a.GitLinked, "git_link_unknown", "git_linked"},
		{a.ServicesKnown, !a.HasServices, "services_unknown", "services_unsupported"},
	}
	for _, predicate := range predicates {
		if !predicate.known {
			reasons = append(reasons, predicate.unknown)
		} else if !predicate.supported {
			reasons = append(reasons, predicate.rejected)
		}
	}
	if a.ProductionDeploymentID == "" {
		reasons = append(reasons, "production_deployment_unknown")
	}
	return reasons
}

func cloneStaticAssessment(in *StaticProjectAssessment) *StaticProjectAssessment {
	if in == nil {
		return nil
	}
	out := *in
	out.Reasons = append([]string{}, in.Reasons...)
	return &out
}
