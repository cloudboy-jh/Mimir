// QueryError carries the HTTP status of a rejected query; routes render it as
// `{ error }` and MCP renders it as a tool error result.
export class QueryError extends Error {
  constructor(
    readonly status: 400 | 404 | 409,
    message: string,
  ) {
    super(message);
  }
}
