CREATE TABLE exchange_tools (
  exchange_id TEXT NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  PRIMARY KEY(exchange_id, name)
);
CREATE INDEX exchange_tools_name ON exchange_tools(name, exchange_id);

-- Recover observed tool names from valid saved excerpts without reading R2.
-- Truncated/unsupported historical archives remain available through raw evidence.
-- Do not treat source-shaped objects inside tool arguments as observed calls.
-- Historical Responses input/output arrays are conservatively left unindexed.
INSERT OR IGNORE INTO exchange_tools(exchange_id, name)
SELECT exchange_id, trim(name)
FROM (
  SELECT exchanges.id AS exchange_id,
    CASE
      WHEN json_extract(node.value, '$.type') IN ('tool_use', 'toolCall', 'tool_call', 'function_call', 'tool', 'toolResult', 'tool_result')
        OR json_extract(node.value, '$.role') = 'tool'
      THEN coalesce(json_extract(node.value, '$.name'), json_extract(node.value, '$.tool'), json_extract(node.value, '$.tool_name'))
      WHEN json_type(node.value, '$.function.arguments') IS NOT NULL OR node.fullkey LIKE '%.tool_calls[%'
      THEN json_extract(node.value, '$.function.name')
    END AS name
  FROM exchanges, json_tree(CASE WHEN json_valid(request_excerpt) THEN request_excerpt ELSE '{}' END) AS node
  WHERE exchanges.capture_status = 'saved' AND node.type = 'object'
    AND node.fullkey NOT LIKE '%.tools[%'
    AND node.fullkey NOT LIKE '%.tools.%'
    AND node.fullkey NOT LIKE '%.parameters.%' AND node.fullkey NOT LIKE '%.parameters[%'
    AND node.fullkey NOT LIKE '%.schema.%' AND node.fullkey NOT LIKE '%.schema[%'
    AND node.fullkey NOT LIKE '%.function.%'
    AND node.fullkey NOT LIKE '%.arguments[%'
    AND node.fullkey NOT LIKE '%.input[%'
    AND node.fullkey NOT LIKE '%.output[%'
    AND node.fullkey NOT LIKE '%.arguments.%'
    AND node.fullkey NOT LIKE '%.input.%'
    AND node.fullkey NOT LIKE '%.output.%'
  UNION ALL
  SELECT exchanges.id AS exchange_id,
    CASE
      WHEN json_extract(node.value, '$.type') IN ('tool_use', 'toolCall', 'tool_call', 'function_call', 'tool', 'toolResult', 'tool_result')
        OR json_extract(node.value, '$.role') = 'tool'
      THEN coalesce(json_extract(node.value, '$.name'), json_extract(node.value, '$.tool'), json_extract(node.value, '$.tool_name'))
      WHEN json_type(node.value, '$.function.arguments') IS NOT NULL OR node.fullkey LIKE '%.tool_calls[%'
      THEN json_extract(node.value, '$.function.name')
    END AS name
  FROM exchanges, json_tree(CASE WHEN json_valid(response_excerpt) THEN response_excerpt ELSE '{}' END) AS node
  WHERE exchanges.capture_status = 'saved' AND node.type = 'object'
    AND node.fullkey NOT LIKE '%.tools[%'
    AND node.fullkey NOT LIKE '%.tools.%'
    AND node.fullkey NOT LIKE '%.parameters.%' AND node.fullkey NOT LIKE '%.parameters[%'
    AND node.fullkey NOT LIKE '%.schema.%' AND node.fullkey NOT LIKE '%.schema[%'
    AND node.fullkey NOT LIKE '%.function.%'
    AND node.fullkey NOT LIKE '%.arguments[%'
    AND node.fullkey NOT LIKE '%.input[%'
    AND node.fullkey NOT LIKE '%.output[%'
    AND node.fullkey NOT LIKE '%.arguments.%'
    AND node.fullkey NOT LIKE '%.input.%'
    AND node.fullkey NOT LIKE '%.output.%'
)
WHERE typeof(name) = 'text' AND length(trim(name)) BETWEEN 1 AND 128;
