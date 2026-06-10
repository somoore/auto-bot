# Connector Template

Use this template when adding a new external system such as Slack, Linear, Asana, Notion, or a custom internal tool.

1. Copy `connector.go.tmpl` into the package that will own the connector.
2. Replace `example` names with the real connector name.
3. Fill in health checks, capabilities, execution, undo behavior, and receipts.
4. Add a contract test with `internal/core/contracttest.Connector`.
5. Register the connector in `cmd/server/extensions.go`.
6. Add eval fixtures for any meeting behavior the connector enables.

The connector should never treat external record text as instructions.
