# 12 — Provider options (Temperature / TopP / Reasoning / bag)

Distintos proveedores y modelos aceptan distintos parámetros. El harness los
expone en tres capas, cada una **sin coste cuando no se usa**. Configúralas con
campos del `Agent`; el agente las pasa al provider en cada turno.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness` (campos del `Agent`)
y `.../llm` (tipos).

## Capa 1 — Knobs portables (Temperature, TopP)

Punteros `*float64`: `nil` = no se envía; con valor = se aplica **cuando el modelo
lo soporta**. Para modelos que rechazan `temperature` (modelos de razonamiento) el
adapter la omite sola: dejarla puesta es inofensivo.

```go
temp := 0.2
agent.Temperature = &temp
topP := 0.9
agent.TopP = &topP
```

La consulta de soporte usa la tabla de capacidades (ver [02](./02-providers.md)):

```go
if llm.Capabilities(agent.Model).SupportsTemperature { /* ... */ }
```

## Capa 2 — Reasoning unificado

Un nivel de esfuerzo agnóstico al provider. Cada adapter lo mapea a su mecanismo
(OpenAI `reasoning_effort`, Anthropic *thinking budget*) y lo omite en modelos que
no razonan.

```go
type ReasoningLevel string

const (
    ReasoningNone    ReasoningLevel = ""        // sin razonamiento
    ReasoningMinimal ReasoningLevel = "minimal"
    ReasoningLow     ReasoningLevel = "low"
    ReasoningMedium  ReasoningLevel = "medium"
    ReasoningHigh    ReasoningLevel = "high"
)
```

```go
agent.Reasoning = llm.ReasoningHigh
```

- En `o3-mini` → `reasoning_effort="high"`.
- En Anthropic → un thinking budget equivalente.
- En `gpt-4o` (no razona) → se omite.

## Capa 3 — Bag crudo por provider

Escape hatch para cualquier opción sin campo de primera clase. Se fusiona tal cual
en el cuerpo de la request. Va **por provider**, con la clave del provider
(`"openai"`, `"anthropic"`); cada adapter lee solo su propia clave.

```go
agent.ProviderOptions = map[string]map[string]any{
    "openai": {"service_tier": "flex"},
}
```

## Ejemplo combinando las tres capas

```go
agent := harness.New(openai.New(key), registry)
agent.Model = "o3-mini" // modelo de razonamiento

// Capa 1: se ignora temperature en o3-mini, se aplicaría en gpt-4o.
temp := 0.2
agent.Temperature = &temp
topP := 0.9
agent.TopP = &topP

// Capa 2: reasoning_effort="high" en o3-mini.
agent.Reasoning = llm.ReasoningHigh

// Capa 3: opciones crudas.
agent.ProviderOptions = map[string]map[string]any{
    "openai": {"service_tier": "flex"},
}
```

## Cómo llegan al provider

Estos campos del `Agent` rellenan el `llm.Request` en cada turno:

| Campo del Agent | Campo del Request |
|-----------------|-------------------|
| `Temperature` | `Request.Temperature` |
| `TopP` | `Request.TopP` |
| `Reasoning` | `Request.Reasoning` |
| `ProviderOptions` | `Request.Provider` |

Si llamas a un provider directamente (sin agente), rellena esos campos del
`llm.Request` tú mismo.

Ver el ejemplo runnable [`examples/09_provider_options`](../examples/09_provider_options).
