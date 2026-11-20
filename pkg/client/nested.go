package client

import "context"

type NestedSide struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	WorkspaceID string `json:"workspaceId"`
	Path        string `json:"path"`
}

type NestedApp struct {
	Name      string `json:"name"`
	Workdir   string `json:"workdir"`
	Status    string `json:"status"`
	ParentApp string `json:"parentApp"`
	Detail    string `json:"detail"`
}

type NestedProject struct {
	Child   NestedSide  `json:"child"`
	Parent  NestedSide  `json:"parent"`
	Apps    []NestedApp `json:"apps"`
	Running int         `json:"running"`
}

type MergeReport struct {
	Child       NestedSide        `json:"child"`
	Parent      NestedSide        `json:"parent"`
	Apps        []NestedApp       `json:"apps"`
	Renamed     map[string]string `json:"renamed"`
	Credentials int               `json:"credentials"`
	Running     int               `json:"running"`
	Applied     bool              `json:"applied"`
	Revision    int64             `json:"revision"`
	Moved       int               `json:"credentialsMoved"`
	Skipped     int               `json:"credentialsSkipped"`
	YAML        string            `json:"yaml"`
}

func (c *ProjectServiceClient) Nested(ctx context.Context) ([]NestedProject, error) {
	var out struct {
		Nested []NestedProject `json:"nested"`
	}
	if err := c.client.call(ctx, "ProjectService", "ListNestedProjects", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Nested, nil
}

func (c *ProjectServiceClient) MergeNested(ctx context.Context, childProjectID, parentProjectID string, dryRun bool) (*MergeReport, error) {
	var out MergeReport
	req := map[string]any{"childProjectId": childProjectID, "parentProjectId": parentProjectID, "dryRun": dryRun}
	if err := c.client.call(ctx, "ProjectService", "MergeNestedProject", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
