# Model Provider Template

Use this template only when adding a governed model backend. Production agent runs currently use AWS Bedrock.

New model providers must preserve:

- Structured JSON output for agent planning and review.
- Cost/capability documentation.
- No direct external action execution.
- Tool calls only through the connector and ledger path.
