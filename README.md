# Atrio

**Plataforma open source de desarrollo asistido por agentes de IA, de ejecución local.**

Atrio inicializa y gobierna monorepos autocontenidos donde el repositorio es la fuente de verdad de todo el proyecto: código de todas las capas, documentación viva y estado de gestión (tareas, decisiones, bitácora). Orquesta las CLIs de agentes de IA instaladas en tu máquina (Claude Code primero; Copilot y Antigravity en el roadmap) mediante agentes especializados por rol, con flujos declarativos que cubren el ciclo de vida completo: ideación → definición → análisis técnico → backlog → implementación.

## Principios

- **Agnóstico al proveedor de IA** — los flujos no dependen del motor concreto.
- **Portable por repositorio** — clonar + `atrio sync` retoma el proyecto íntegro.
- **Autonomía gobernada** — el usuario decide qué ejecuta cada agente automáticamente y qué requiere autorización.
- **Código antes que agentes** — todo lo determinístico (índices, validaciones, scaffold, sync) es código, no prompts.
- **Contexto eficiente** — documentación fragmentada e indexada; los agentes cargan solo lo que necesitan.

## Estado

🚧 En desarrollo — implementando M1 (inicialización completa por CLI). La especificación completa vive en [`docs/spec/`](docs/spec/):

1. [Definición de producto](docs/spec/01-definicion-producto.md)
2. [Registro de decisiones (ADRs)](docs/spec/02-registro-adr.md)
3. [Arquitectura](docs/spec/03-arquitectura.md)
4. [Backlog M1](docs/spec/04-backlog-m1.md)

## Licencia

[Apache-2.0](LICENSE)
