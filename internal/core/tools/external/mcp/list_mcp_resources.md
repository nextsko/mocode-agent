List available resource URIs from an MCP server by name. Only use this when you don't already know the resource URI.

<when_to_use>
Use this tool ONLY when you need to discover available resources and don't already have a specific URI. If you already know the resource URI, skip this step and use read_mcp_resource directly.
</when_to_use>

<usage>
- Provide MCP server name
- Returns resource titles and URIs
</usage>

<parameters>
- mcp_name: The MCP server name
</parameters>

<notes>
- Results include resource titles, URIs, and metadata when available
</notes>
