# harness — Documentación por funcionalidad

Guía práctica para construir tus propios agentes con el `harness`. Cada página
documenta **una** funcionalidad: qué es, la API pública exacta, y ejemplos de
código copiables.

Si es tu primera vez, lee en orden **01 → 04**; el resto son piezas opcionales
que añades solo cuando las necesitas.

## Núcleo (empieza aquí)

| # | Página | Qué cubre |
|---|--------|-----------|
| 01 | [Agent](./01-agent.md) | El bucle del agente: `New`, `Run`, campos de configuración, estado de la conversación. |
| 02 | [Providers](./02-providers.md) | Conectar OpenAI / Anthropic, la interfaz `Provider`, mensajes y `Usage`. |
| 03 | [Tools](./03-tools.md) | La interfaz `Tool`, el `Registry`, y las herramientas built-in (Read/Write/Edit/…/Bash). |
| 04 | [Backends: fsys + exec](./04-backends.md) | Dónde viven los ficheros y dónde corre el shell. `Files` / `Default` / `FromExecutor`. |

## Piezas opcionales (lego)

| # | Página | Qué cubre |
|---|--------|-----------|
| 05 | [Retry & Router](./05-retry-router.md) | Reintentos con backoff y enrutado de varios modelos a su provider. |
| 06 | [Hooks](./06-hooks.md) | Observabilidad: turnos, herramientas, uso de tokens, reintentos, compactación. |
| 07 | [Compaction](./07-compaction.md) | Mantener conversaciones largas dentro de la ventana de contexto. |
| 08 | [ToolSearch](./08-toolsearch.md) | Diferir esquemas de herramientas para catálogos grandes. |
| 09 | [Skills](./09-skills.md) | Cargar conjuntos de instrucciones bajo demanda (local / embed / in-code). |
| 10 | [Subagents](./10-subagents.md) | Delegar subtareas aisladas a agentes anidados. |
| 11 | [Todo](./11-todo.md) | Lista de tareas declarativa para el agente. |
| 12 | [Provider options](./12-provider-options.md) | Temperature, TopP, Reasoning y el "bag" crudo por provider. |

## El agente mínimo

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/Abraxas-365/manifesto/internal/ai/harness"
    "github.com/Abraxas-365/manifesto/internal/ai/harness/exec"
    "github.com/Abraxas-365/manifesto/internal/ai/harness/fsys/fsxstore"
    "github.com/Abraxas-365/manifesto/internal/ai/harness/llm/openai"
    "github.com/Abraxas-365/manifesto/internal/ai/harness/tool/builtins"
    "github.com/Abraxas-365/manifesto/internal/fsx/fsxlocal"
)

func main() {
    fs, _ := fsxlocal.NewLocalFileSystem(".")
    ex := exec.NewLocalExecutor(".")

    registry := builtins.Default(fsxstore.New(fs), ex)   // Read, Write, Edit, List, Glob, Grep, Bash
    provider := openai.New(os.Getenv("OPENAI_API_KEY"))

    agent := harness.New(provider, registry)
    agent.System = "You are a helpful coding assistant."
    agent.Model = "gpt-4o"

    answer, _ := agent.Run(context.Background(), "List the Go files and summarize main.go")
    fmt.Println(answer)
}
```

## Principios de diseño

- **Opcional, no incrustado.** Un campo nil es un no-op. Añadir una feature nunca
  cambia el comportamiento del código que no la usa.
- **Providers decoradores.** `retry.Provider` y `router.Router` implementan
  `llm.Provider`, así que se envuelven entre sí.
- **Cambia el entorno, conserva el agente.** Apunta el mismo agente a S3 o a un
  executor remoto cambiando solo el constructor del registry.
- **Falla fuerte al cablear.** P.ej. `EnableToolSearch()` hace panic si
  `SetDeferred` nombra una herramienta que nunca se registró (un typo).

## Ejemplos ejecutables

Los programas completos viven en [`../examples/`](../examples). Cada uno es un
`main` independiente: exporta la API key correspondiente y `go run`.

## Verificación

```
go build ./internal/ai/harness/...
go test  ./internal/ai/harness/... -count=1
```
