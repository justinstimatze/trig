package linear

// Issue is the subset of Linear's Issue type trig needs. Linear answers
// named-type GraphQL introspection against api.linear.app/graphql without
// requiring a key, which is how this shape was derived — see DESIGN.md.
type Issue struct {
	ID          string       `json:"id"`
	Identifier  string       `json:"identifier"`
	Title       string       `json:"title"`
	Labels      []IssueLabel `json:"labels"`
	Attachments []Attachment `json:"attachments"`
}

type IssueLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Attachment struct {
	ID       string                 `json:"id"`
	Title    string                 `json:"title"`
	Subtitle string                 `json:"subtitle"`
	URL      string                 `json:"url"`
	Metadata map[string]interface{} `json:"metadata"`
}

// issueResponse mirrors the raw GraphQL shape (labels/attachments are
// paginated connections with a `nodes` wrapper) so callers get the plain
// Issue type above instead of leaking that wrapper.
type issueResponse struct {
	Issue *struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
		Labels     struct {
			Nodes []IssueLabel `json:"nodes"`
		} `json:"labels"`
		Attachments struct {
			Nodes []Attachment `json:"nodes"`
		} `json:"attachments"`
	} `json:"issue"`
}

type issueLabelsResponse struct {
	IssueLabels struct {
		Nodes []IssueLabel `json:"nodes"`
	} `json:"issueLabels"`
}

type attachmentPayload struct {
	Success    bool       `json:"success"`
	Attachment Attachment `json:"attachment"`
}

type attachmentCreateResponse struct {
	AttachmentCreate attachmentPayload `json:"attachmentCreate"`
}

type attachmentUpdateResponse struct {
	AttachmentUpdate attachmentPayload `json:"attachmentUpdate"`
}

type issueUpdateResponse struct {
	IssueUpdate struct {
		Success bool `json:"success"`
	} `json:"issueUpdate"`
}
