package tools

// AgenticFetchToolName is the name of the agentic fetch tool.
const AgenticFetchToolName = "agentic_fetch"

// WebFetchToolName is the name of the web_fetch tool.
const WebFetchToolName = "web_fetch"

// WebSearchToolName is the name of the web_search tool for sub-agents.
const WebSearchToolName = "web_search"

// LargeContentThreshold is the size threshold for saving content to a file.
const LargeContentThreshold = 50000 // 50KB

// AgenticFetchParams defines the parameters for the agentic fetch tool.
type AgenticFetchParams struct {
	URL    string `json:"url,omitempty" description:"The URL to fetch content from (optional - if not provided, the agent will search the web)"`
	Prompt string `json:"prompt" description:"REQUIRED: The prompt describing what information to find or extract (required, do not omit)"`
}

// AgenticFetchPermissionsParams defines the permission parameters for the agentic fetch tool.
type AgenticFetchPermissionsParams struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt"`
}

// WebFetchParams defines the parameters for the web_fetch tool.
type WebFetchParams struct {
	URL string `json:"url" description:"REQUIRED: The URL to fetch content from (required, do not omit)"`
}

// WebSearchParams defines the parameters for the web_search tool.
type WebSearchParams struct {
	Query      string `json:"query" description:"REQUIRED: The search query to find information on the web (required, do not omit)"`
	MaxResults int    `json:"max_results,omitempty" description:"Maximum number of results to return (default: 10, max: 20)"`
}

// FetchParams defines the parameters for the simple fetch tool.
type FetchParams struct {
	URL     string `json:"url" description:"REQUIRED: The URL to fetch content from (required, do not omit)"`
	Format  string `json:"format" description:"REQUIRED: The format to return the content in (text, markdown, or html) (required, do not omit)"`
	Timeout int    `json:"timeout,omitempty" description:"Optional timeout in seconds (max 120)"`
}

// FetchPermissionsParams defines the permission parameters for the simple fetch tool.
type FetchPermissionsParams struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Timeout int    `json:"timeout,omitempty"`
}
