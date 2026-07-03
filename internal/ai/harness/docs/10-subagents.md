# 10 — Subagents (agentes anidados)

Un subagente es un agente completo que corre como una herramienta. Recibe un
prompt autónomo, trabaja en su **propio historial aislado** y devuelve solo su
respuesta final — sus pasos intermedios nunca ensucian la conversación del padre.
Ideal para investigar o buscar sin inflar el contexto principal.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness/subagent`

## La herramienta

```go
type Tool struct {
    NewAgent      Factory   // construye el agente anidado por cada llamada (requerido)
    ToolName      string    // default "Task" (DefaultName)
    Desc          string    // default DefaultDescription
    AllowedModels []string  // opcional: restringe el parámetro "model" a este set
}

type Factory func() *harness.Agent
```

`NewAgent` se llama **una vez por invocación**, así que cada subtarea empieza con
historial limpio. La herramienta implementa `tool.Tool`, se registra como
cualquier otra.

## Uso básico

```go
registry.Register(&subagent.Tool{
    NewAgent: func() *harness.Agent {
        sub := harness.New(provider, builtins.Default(store, ex))
        sub.System = "Eres un subagente enfocado. Devuelve solo la respuesta final."
        sub.Model = "gpt-4o-mini"
        return sub
    },
})
```

El modelo del padre invoca la herramienta `Task` con un `prompt` autónomo (el
subagente no ve la conversación del padre) y recibe solo el texto final.

## Elegir el modelo del subagente

El parámetro opcional `model` deja que el **modelo padre** elija qué modelo corre
la subtarea. Para que ese modelo se despache al provider correcto, el agente
anidado debe usar un **router** (ver [05](./05-retry-router.md)). Con
`AllowedModels` restringes las opciones a un enum validado en tiempo de llamada.

```go
r := router.New()
r.HandlePattern("gpt-*", openai.New(openaiKey))
r.HandlePattern("claude-*", anthropic.New(anthropicKey))

registry.Register(&subagent.Tool{
    AllowedModels: []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514"},
    NewAgent: func() *harness.Agent {
        sub := harness.New(r, builtins.Default(store, ex))
        sub.System = "Eres un subagente enfocado. Devuelve solo la respuesta final."
        sub.Model = "gpt-4o-mini" // default si el padre no especifica model
        sub.EnableRetry()
        return sub
    },
})
```

- Si el padre pasa `model`, sobrescribe el `Model` que fija `NewAgent`.
- Con `AllowedModels` no vacío, un `model` fuera del set se rechaza.
- `AllowedModels` vacío permite cualquier string de modelo.

## Personalizar nombre y descripción

Registra varias herramientas de subagente con distinto propósito:

```go
registry.Register(&subagent.Tool{
    ToolName: "Research",
    Desc:     "Investiga una pregunta técnica a fondo y devuelve un resumen.",
    NewAgent: newResearchAgent,
})
```

## Notas

- El subagente puede usar herramientas mutantes, así que la herramienta **no** es
  read-only.
- Comparte lo que le des en `NewAgent`: router, tools, retry, hooks. Nada se
  hereda automáticamente del padre.
- Anidamiento: un subagente puede a su vez registrar herramientas de subagente.

Ver el ejemplo runnable [`examples/06_subagent`](../examples/06_subagent).
