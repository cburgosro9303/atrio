# Atrio

Plataforma open source, de ejecución local, para desarrollo de software asistido por agentes de IA. Un ejecutable único en Go (CLI global `atrio` + portal web local con SPA React embebida) que orquesta CLIs de agentes instaladas en la máquina (primer proveedor: Claude Code) sobre monorepos autocontenidos donde el repositorio es la fuente de verdad del estado completo del proyecto.

## Mapa documental (leer bajo demanda, no cargar todo)

- `docs/spec/01-definicion-producto.md` — Visión, principios, reglas de negocio, roadmap M1→F4. Consultar para cualquier duda funcional o de alcance.
- `docs/spec/02-registro-adr.md` — ADRs 001–015 con contexto y alternativas. Consultar antes de cuestionar una decisión técnica.
- `docs/spec/03-arquitectura.md` — **Documento normativo de diseño**: topología, almacenes, paquetes, contrato del adaptador, esquemas, motor de flujos, modelo git, seguridad. Consultar SIEMPRE antes de decisiones de diseño o de crear un módulo nuevo.
- `docs/spec/04-backlog-m1.md` — Backlog ordenado por dependencias. Es la fuente de qué trabajar y en qué orden.

## Reglas innegociables

### Lenguaje y estructura
- Core en **Go**. Frontend del portal en React 19.x + TypeScript + Vite 8.x, embebido en el binario.
- Paquetes: `core/`, `store/`, `gitops/`, `providers/`, `flows/`, `api/`, `cli/`, `web/`.
- **Dependencias unidireccionales**: `core/` no importa ningún otro paquete del proyecto (dominio puro, sin I/O). Ningún paquete importa `cli/` ni `api/`. Violación = rechazo del cambio.
- **Prohibido CGo**: rompe el cross-compile. SQLite se usa vía `modernc.org/sqlite` (driver Go puro).
- Código, identificadores y comentarios en **inglés**. Documentación de especificación en español (soporte bilingüe es→en llegará por i18n, no por traducción de código).

### Seguridad (prioridad #1 del proyecto)
- **Nunca shell interpolado**: toda invocación de procesos externos (git incluido) por array de argumentos.
- Git se delega al binario del sistema con salidas `--porcelain`; jamás bibliotecas git embebidas.
- El portal solo hace bind a localhost, exige token de sesión y ninguna acción destructiva por GET.

### Calidad
- `go test -race` obligatorio; los tests acompañan cada tarea, no se difieren.
- Todo artefacto JSON valida contra su JSON Schema; los esquemas llevan `schemaVersion`.
- IDs: ULID en todos los artefactos.

### Flujo de trabajo (dogfooding de las propias reglas de Atrio)
- Trabajar **una tarea del backlog por sesión**, respetando el orden de dependencias de `04-backlog-m1.md`.
- Una tarea = una rama: `task/{id-corto}-{slug}` (ej. `task/t001-bootstrap`).
- Al completar una tarea: actualizar su estado en `docs/spec/04-backlog-m1.md` en el mismo cambio.
- Commits atómicos con mensaje que referencia la tarea (ej. `T-001: bootstrap Go module and package skeleton`).
- **Push y comandos fuera del allowlist requieren aprobación del usuario** (configurado en `.claude/settings.json`).
- Para tareas de diseño (T-010, T-011, T-012, T-050): proponer plan y esperar aprobación antes de escribir código.

## Orquestación de modelos

- La **sesión principal corre en Opus** y actúa como orquestador: toma decisiones de diseño, planifica, y delega ejecución.
- Los **subagentes del proyecto** (`.claude/agents/`) corren en **claude-sonnet-5**: `explorer` (exploración de código/specs, solo lectura, protege el contexto principal), `implementer` (ejecución de tareas ya diseñadas, ideal para las marcadas ∥ en paralelo), `code-reviewer` (revisión contra las reglas duras antes de cada commit).
- Reparto: razonamiento arquitectónico y tareas de diseño (T-010, T-011, T-012, T-050) en la sesión principal; exploración, implementación mecánica y revisión delegadas a subagentes.
- Usa `code-reviewer` proactivamente antes de solicitar aprobación de cualquier commit.

## Comandos del proyecto

Se definirán en T-001 (Makefile/Taskfile). Hasta entonces: `go build ./...`, `go test -race ./...`, `go vet ./...`.

## Estado actual

Repositorio recién inicializado. Próxima tarea: **T-001 (bootstrap del repositorio)**. Riesgo prioritario a despejar temprano: spike de T-053 (arbitraje de permisos vía hooks de Claude Code) antes de congelar el diseño fino de T-050.
