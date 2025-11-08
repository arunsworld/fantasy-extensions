# Fantasy Extensions

Extend capabilities of the [Fantasy agent framework](https://github.com/charmbracelet/fantasy).

# MCP Extension

```
    tools, err := fantasyextensions.MCPTools(context.Background(), sessionMaker)
    ...
    // use tools within agent
    fantasy.NewAgent(model, fantasy.WithTools(tools...))
```

# AGUI Extension

```
    handler := AGUIHandler(model, systemPromptGenerator, tools...)
```