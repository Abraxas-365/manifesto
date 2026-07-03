# 04 — Backends (fsys + exec)

El harness separa *dónde viven los ficheros* de *dónde se ejecutan los comandos*
en dos interfaces estrechas:

- `fsys.Store` — operaciones de fichero (Read/Write/Edit/List/Glob/Grep).
- `exec.Executor` — comandos de shell (Bash).

Ambas son intercambiables. Puedes correr en local, en Docker, sobre S3, sobre
SSH, o en un mock para tests.

Paquetes:
- `.../fsys` — interfaz `Store`
- `.../fsys/fsxstore` — adaptador de un `fsx.FileSystem` a `Store`
- `.../fsys/execstore` — `Store` derivado de un `exec.Executor`
- `.../exec` — interfaz `Executor` y `LocalExecutor`

## `fsys.Store`

Una interfaz mínima (5 métodos), justo lo que necesitan las 6 herramientas de
fichero — nada más.

```go
type Store interface {
    ReadFile(ctx context.Context, path string) ([]byte, error)
    WriteFile(ctx context.Context, path string, data []byte) error
    Stat(ctx context.Context, path string) (FileInfo, error)
    List(ctx context.Context, path string) ([]FileInfo, error)
    MkdirAll(ctx context.Context, path string) error
}

type FileInfo struct {
    Name  string
    Size  int64
    IsDir bool
}

var ErrNotExist = errors.New("file does not exist")
```

Una implementación debe devolver `ErrNotExist` (envuelto o no) cuando un path no
existe, para que las herramientas distingan "no existe" de un fallo de transporte.

### Implementaciones incluidas

**fsxstore** — envuelve cualquier `fsx.FileSystem` (local, S3, memoria):

```go
func New(fs fsx.FileSystem) fsys.Store
```

```go
fs, _ := fsxlocal.NewLocalFileSystem(".")
store := fsxstore.New(fs)
```

**execstore** — deriva el `Store` de un `exec.Executor`, ejecutando comandos POSIX
portables (`base64`, `wc`, `find`) por debajo:

```go
func New(ex exec.Executor) fsys.Store
```

Con esto las operaciones de fichero ocurren *en el mismo entorno* que Bash. Los
datos binarios viajan en base64. Un `WriteFile` está limitado a
`execstore.MaxWriteBytes` (256 KB) porque el payload va inline en el comando.

## `exec.Executor`

```go
type Executor interface {
    Run(ctx context.Context, command string, opts RunOptions) (*RunResult, error)
}

type RunOptions struct {
    WorkDir string
    Timeout time.Duration   // 0 = default del executor
    Env     []string        // "KEY=VALUE"
}

type RunResult struct {
    Stdout   string
    Stderr   string
    ExitCode int
    TimedOut bool
}
```

### LocalExecutor

```go
func NewLocalExecutor(workDir string) *LocalExecutor
```

`DefaultTimeout = 120s` cuando `RunOptions.Timeout` es 0. Ejecuta con `bash -c`
gestionando el grupo de procesos.

### Background (opcional)

Un executor puede además implementar `BackgroundExecutor` para lanzar procesos
detached (servidores, watchers):

```go
type BackgroundExecutor interface {
    Executor
    Start(ctx context.Context, command string, opts RunOptions) (string, error)
    Poll(id string) (*BackgroundStatus, error)
    Kill(id string) error
}
```

`LocalExecutor` lo implementa. Cuando el executor lo soporta, `builtins.Default`
y `builtins.FromExecutor` registran automáticamente las herramientas `BashOutput`
y `KillShell`.

## El problema del "split-brain"

Si el `Store` y el `Executor` apuntan a entornos distintos, el agente lee un
mundo y ejecuta en otro: edita un fichero local pero compila dentro de un
contenedor, y nunca ve sus propios cambios.

**Regla de emparejamiento:**

| Backend de ejecución | Cómo construir el registry |
|----------------------|-----------------------------|
| Solo almacenamiento (S3), sin shell | `builtins.Files(store)` |
| Local, un solo entorno | `builtins.FromExecutor(exec.NewLocalExecutor("."))` |
| Docker / SSH / remoto | `builtins.FromExecutor(remoteExecutor)` |
| Store y executor emparejados a mano (avanzado) | `builtins.Default(store, ex)` |

`FromExecutor` deriva el store del mismo executor (vía `execstore`), así que el
split-brain es **imposible por construcción**. Úsalo salvo que tengas una razón
deliberada para separar los backends.

## Ejemplos runnable

- [`examples/01_minimal`](../examples/01_minimal) — local con `fsxstore`.
- [`examples/11_s3`](../examples/11_s3) — solo S3 con `builtins.Files`.
- [`examples/12_docker`](../examples/12_docker) — Docker con `builtins.FromExecutor`.
- [`examples/13_fromexecutor`](../examples/13_fromexecutor) — un solo backend local.
