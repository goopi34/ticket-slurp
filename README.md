# ticket-slurp

Harvests Slack conversations you participated in, uses an LLM to identify potential engineering work items, cross-references them against Jira to filter out already-tracked tickets, and reports the delta.

## How it works

1. Fetches messages from Slack channels you're a member of, within a configurable timeframe
2. Sends conversations to an LLM to identify bug reports, feature requests, tech debt, blocked work, infrastructure issues, and security concerns
3. Queries Jira (via a local MCP server) to find tickets that already cover each candidate
4. Outputs a Markdown or JSON report of untracked work

## Prerequisites

- Go 1.22+
- Docker (for the Jira MCP server)
- Slack desktop client tokens (`xoxc` + `xoxd`)
- A Jira instance with API access
- An LLM provider (Azure OpenAI, OpenAI, Anthropic, or Ollama)

## Setup

### 1. Start the Jira MCP server

The MCP server handles all Jira communication. It runs as a local Docker container.

```sh
cp docker/atlassian-mcp/.env.example docker/atlassian-mcp/.env
```

Edit `.env` with your Jira credentials. For Jira Server/Data Center with a PAT:

```sh
JIRA_URL=https://jira.yourcompany.com
JIRA_PERSONAL_TOKEN=<your-personal-access-token>
```

For Jira Cloud with an API token:

```sh
JIRA_URL=https://yourcompany.atlassian.net
JIRA_USERNAME=you@yourcompany.com
JIRA_API_TOKEN=<your-api-token>
```

Then start the server:

```sh
make mcp-up      # start in background
make mcp-logs    # tail logs
make mcp-down    # stop and remove
```

The server listens at `http://localhost:9000/mcp` by default.

### 2. Configure ticket-slurp

```sh
cp config.example.yaml ticket-slurp.yaml
```

Edit `ticket-slurp.yaml`:

```yaml
slack:
  xoxc: "xoxc-..."   # from Slack desktop client
  xoxd: "xoxd-..."   # from Slack desktop client

timeframe:
  start: "2026-03-27"
  end: "2026-04-03"
  # or use a relative window:
  # last_days: 7

channels:
  whitelist: []   # if set, only these channel IDs are analysed
  blacklist: []   # always skip these channel IDs

llm:
  provider: "azure"   # azure | openai | anthropic | ollama
  azure:
    endpoint: "https://<resource>.openai.azure.com/"
    api_key: "${AZURE_OPENAI_API_KEY}"
    deployment: "gpt-4o"

atlassian:
  mcp_url: "http://localhost:9000"
  project_keys:
    - "ENG"
    # - "OPS"

output:
  format: "markdown"   # markdown | json
```

API keys can be supplied inline or via environment variables using `${ENV_VAR}` syntax.

#### Getting Slack tokens

The `xoxc` and `xoxd` tokens come from the Slack desktop app's local storage or network requests. They are not available from the Slack API settings page.

#### Getting a Jira PAT (Server/Data Center)

Jira → your profile → **Personal Access Tokens** → **Create token**.

#### Getting a Jira API token (Cloud)

Visit `https://id.atlassian.com/manage-profile/security/api-tokens`.

### 3. Build

```sh
make build
```

The binary is written to `bin/ticket-slurp`.

## Usage

```sh
bin/ticket-slurp run
```

By default the tool looks for `ticket-slurp.yaml` in the current directory. Pass `--config` to use a different path:

```sh
bin/ticket-slurp run --config /path/to/my-config.yaml
```

The report is written to stdout. Redirect it to a file as needed:

```sh
bin/ticket-slurp run > report.md
```

## Output

### Markdown (default)

```
# Untracked Engineering Work

Generated: 2026-04-06 | Candidates found: 12 | Need tickets: 5 | Already tracked: 7

| # | Title | Priority | Channel | Rationale |
|---|-------|----------|---------|-----------|
| 1 | Auth service intermittently returns 503 | high | #backend | ... |
...
```

### JSON

```sh
bin/ticket-slurp run --config ticket-slurp.yaml  # set output.format: json in config
```

```json
{
  "generated_at": "2026-04-06T12:00:00Z",
  "total_identified": 12,
  "need_tickets": 5,
  "candidates": [...]
}
```

## Development

```sh
make vet          # static analysis
make lint         # golangci-lint
make test         # tests with race detector + coverage
make check        # vet + lint + test
make cover        # open HTML coverage report
make install-tools  # install golangci-lint and goreleaser
```

## Configuration reference

| Field | Required | Description |
|---|---|---|
| `slack.xoxc` | yes | Slack desktop client token |
| `slack.xoxd` | yes | Slack desktop client token |
| `slack.user_id` | no | Filter messages to a specific Slack user ID |
| `timeframe.start` / `timeframe.end` | yes* | Explicit date range (YYYY-MM-DD, inclusive) |
| `timeframe.last_days` | yes* | Relative window; mutually exclusive with start/end |
| `channels.whitelist` | no | If non-empty, only these channel IDs are analysed |
| `channels.blacklist` | no | Always excluded channel IDs |
| `llm.provider` | yes | `azure` \| `openai` \| `anthropic` \| `ollama` |
| `llm.system_prompt` | no | Overrides the built-in analysis prompt entirely |
| `atlassian.mcp_url` | yes | URL of the running MCP server |
| `atlassian.project_keys` | yes | List of Jira project keys to search within |
| `output.format` | no | `markdown` (default) \| `json` |

\* One of `start`+`end` or `last_days` is required.
