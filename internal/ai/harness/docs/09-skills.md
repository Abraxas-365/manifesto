# 09 — Skills (instrucciones bajo demanda)

Un *skill* es un conjunto de instrucciones especializadas que el agente carga solo
cuando las necesita. Consiste en un `SKILL.md` (un cuerpo corto + frontmatter) más
ficheros de referencia opcionales. Al invocarlo, sus ficheros se materializan en un
directorio local real y el cuerpo se devuelve con `${SKILL_DIR}` sustituido, así el
agente puede `Read`/`Bash` las referencias.

Paquete: `github.com/Abraxas-365/manifesto/internal/ai/harness/skill`

## Piezas

- `Skill` — una skill cargada (metadata + cuerpo + fuente).
- `Registry` — colección de skills disponibles.
- `Tool` — la herramienta `Skill` que se registra en el agente.
- `Cache` — dueño de los directorios materializados.

## Cargar una skill

Tres fuentes, según de dónde vengan los ficheros:

```go
func FromFS(ctx, fs fsx.FileSystem, dir string) (*Skill, error)     // local o S3
func FromEmbed(ctx, fs embed.FS, dir string) (*Skill, error)        // go:embed
func FromStatic(ctx, s *Static) (*Skill, error)                     // en código
func LoadDir(ctx, fs fsx.FileSystem, root string) ([]*Skill, error) // todas las de un dir
```

### En código (Static)

```go
static := &skill.Static{
    Name:        "go-style",
    Description: "Reglas de estilo Go. Úsala al escribir o revisar código Go.",
    Body: "# Go Style\n\nSigue las reglas en ${SKILL_DIR}/references/style.md.",
    References: map[string][]byte{
        "references/style.md": []byte("- Envuelve errores con %w.\n- Tests table-driven.\n"),
    },
}
sk, err := skill.FromStatic(ctx, static)
```

### Desde disco o S3

```go
sk, _ := skill.FromFS(ctx, fs, ".claudio/skills/manifesto")
```

### Desde un embed.FS

```go
//go:embed skills/go-style
var embedded embed.FS
sk, _ := skill.FromEmbed(ctx, embedded, "skills/go-style")
```

## Registrar y exponer al agente

```go
skReg := skill.NewRegistry()
skReg.Register(sk)

skillTool := &skill.Tool{Registry: skReg}
defer skillTool.Close() // elimina el dir efímero de materialización

registry := builtins.Default(fsxstore.New(fs), ex)
registry.Register(skillTool)

agent := harness.New(openai.New(key), registry)
agent.System = "Cuando una tarea necesite convenciones de la casa, carga la skill correspondiente."
```

La `Description` de la herramienta `Skill` lista automáticamente las skills
disponibles con su nombre y descripción, así el modelo sabe qué puede cargar.

## Cómo funciona la materialización

La primera vez que el modelo invoca una skill, sus ficheros se copian a un
directorio temporal (el `Cache`). El cuerpo devuelto tiene `${SKILL_DIR}`
reemplazado por esa ruta, así que el agente puede leer o ejecutar las referencias.
Fuentes no locales (embed, S3, static) se copian de forma perezosa en la primera
invocación.

- `Cache` nil → se crea un cache efímero. `skillTool.Close()` borra el directorio.
- Para conservar los ficheros entre ejecuciones, pasa un `Cache` persistente con
  `skill.NewCache(baseDir)`.

## Frontmatter de un SKILL.md

```markdown
---
name: go-style
description: Reglas de estilo Go. Úsala al escribir o revisar código Go.
---

# Go Style

Sigue las reglas en ${SKILL_DIR}/references/style.md antes de escribir código.
```

Ver el ejemplo runnable [`examples/05_skills`](../examples/05_skills).
