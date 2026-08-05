# Documento de Arquitectura — Plataforma de Desarrollo Asistido por Agentes

**Documento consolidado de la sesión de definición técnica y arquitectónica**
Estado: Sesión técnica cerrada · Listo para implementación de M1
Complementa a: *Definición de Producto* · Absorbe y actualiza el *Registro de Decisiones Técnicas (ADRs 001–014)*

---

## 1. Visión técnica

Ejecutable único en **Go** que actúa como CLI global (modelo Angular CLI) y sirve un portal web local (**SPA React embebida**). Orquesta **CLIs de agentes de IA instaladas en la máquina** (primer proveedor: Claude Code) a través de un contrato de adaptador agnóstico. El **repositorio del proyecto es la fuente de verdad** del estado compartido (artefactos JSON versionados + documentos markdown); una base **SQLite local no versionada** guarda exclusivamente estado derivable o efímero. **Git del sistema** es la columna vertebral de aislamiento, trazabilidad y sincronización de equipo. Los **flujos son definiciones declarativas** ejecutadas por un motor genérico determinístico; los agentes conversan y producen, nunca deciden el rumbo.

## 2. Stack y decisiones fundamentales

| Dimensión | Decisión | ADR |
|---|---|---|
| Runtime del core | Go (binario único estático, cross-compile, goroutines para supervisión de agentes, comunidad del nicho, óptimo para desarrollo asistido por agentes) | 001 |
| Frontend | React 19.x + TypeScript + Vite 8.x, SPA embebida en el binario | 008 |
| Persistencia compartida | JSON + JSON Schema, versionado en el repo | 005, 006 |
| Persistencia local | SQLite 3.x, driver puro Go `modernc.org/sqlite`, FTS5 | 006, 007 |
| Git | Delegación al binario del sistema, envoltura segura (arrays de argumentos, `--porcelain`) | 003 |
| Primer proveedor | Claude Code; interfaz validada contra convenciones de Copilot y Antigravity | 004 |
| Distribución | GitHub Releases + Homebrew/winget/script Linux + `upgrade` integrado | 009 |
| Portal↔core | HTTP+JSON local con token de sesión; SSE para tiempo real | 010 |
| IDs | ULID en todos los artefactos | 014 |

## 3. Topología

- **Un ejecutable**: la CLI es la puerta de entrada; el comando de portal levanta servidor HTTP en localhost que sirve la SPA y expone la misma API interna que consume la CLI. Cero lógica de negocio en el frontend.
- **Sin daemon obligatorio**: cualquier proceso puede morir; el estado sobrevive en disco.
- **Ejecuciones de agentes**: proceso supervisor efímero por ejecución.
- **Concurrencia** (CLI, portal, agentes paralelos): locks por proyecto (lockfiles), registrados en SQLite.
- **Todo artefacto lleva `schemaVersion`** desde el día uno.

## 4. Modelo de almacenamiento (tres almacenes)

| Almacén | Contenido | En el repo |
|---|---|---|
| **JSON versionado** (carpeta de gestión) | Tareas, decisiones, bitácora, changelogs, progreso de flujos, configuración, declaración de agentes, personalización, permisos | ✅ |
| **SQLite local** (directorio de plataforma por proyecto, en `.gitignore` generado por `init`) | Índice documental + FTS, caché de metadatos, hashes de materialización, sesión en curso, notificaciones, locks, tokens de sesión, ejecuciones en vivo | ❌ |
| **Documentos legibles** (`docs/`) | Corpus markdown del proyecto del desarrollador, con metadatos en front-matter | ✅ |

**Invariante verificable (test en CI)**: `borrar SQLite + clonar + sync` reconstruye el 100% del estado local.

## 5. Estructura del monorepo generado por `init`

**Nombre del producto: Atrio** (ADR-015). Comando de la CLI: `atrio`. Raíz del monorepo generado: `docs/` · `.atrio/` (`config.json`, `agents.json`, `management/` con un archivo JSON por entidad, `atrio.db` local ignorado por git) · carpetas de capas creadas solo tras la definición bloqueante de stack · carpetas de proveedores (`.claude/`, …) en la raíz por convención de sus CLIs.

## 6. Descomposición del core Go

Paquetes con dependencias unidireccionales: `core/` (dominio puro, sin I/O) · `store/` (JSON + SQLite + validación de esquemas) · `gitops/` · `providers/` (interfaz + `claudecode/`) · `flows/` (motor genérico) · `api/` (HTTP+SSE) · `cli/` · `web/` (SPA embebida). Reglas: `core` no importa nada; nadie importa `cli` ni `api`.

## 7. Contrato del adaptador de proveedores

Cinco responsabilidades: **(1) Identidad/descubrimiento** (CLI instalada, versión, compatibilidad); **(2) Capacidades** contra catálogo cerrado gobernado por la plataforma (`agentes`, `skills`, `hooks`, `ejecución_headless`, `salida_estructurada`, `sesión_interactiva`, `reporte_de_tokens`, `contexto_por_archivo`, …); **(3) Materialización** — compila la definición canónica agnóstica (JSON propio) al dialecto nativo y reporta deriva; **(4) Ejecución** — modos headless y sesión conversacional, ambos emitiendo un stream de eventos normalizados (inicio, texto, uso de herramienta, solicitud de autorización, fin, error, métricas); **(5) Métricas** — tokens aproximados si el proveedor los expone, nunca estimados inventando.

Reglas transversales:
- **Permisos arbitrados en el core**: el adaptador reporta intenciones como eventos; el core decide según las 7 categorías × 3 niveles. Comportamiento idéntico en todos los proveedores. En M1, transporte vía hooks/modo de aprobación de Claude Code.
- **Contramedidas como código del core**: estrategias de degradación por capacidad (`skills → inyección en prompt`; `hooks → advertir y omitir`), uniformes para todo adaptador.
- **Rangos de versiones soportadas** por adaptador; fuera de rango: aviso y degradación a "no disponible". Tests contra versiones fijadas en CI.
- **Requisito verificable de extensibilidad**: agregar un proveedor = implementar interfaz + registrar; cero cambios en core, flujos o comandos.
- Alcance M1: detección, matriz, materialización+deriva, headless, sesión de un agente. Mesas multi-agente en M2 sobre el mismo contrato.

## 8. Esquemas de artefactos

Sobre común: `schemaVersion`, `id` (ULID), `createdAt/updatedAt`, `createdBy` (identidad git o agente).

- **Tarea** (`task | bug`): `title`, `description`, `epicId?`, `tags[]`, `state` + `stateHistory[]` embebido (`{de, a, quién, cuándo, motivo?}`), `blockedBy[]` tipado (obligatorio en bloqueada), `assignee?`, `closureEnvironment`, `environmentProgress[]` con `confirmedBy` humano, `branch?/worktree?`, `changelogRefs[]`. Bugs nacen `triage` con `originTaskRef?`. Estados definitivos inmutables salvo referencias entrantes.
- **Decisión**: `context`, `decision`, `consequences[]`, `alternativesConsidered[]?`, `status: activa|reemplazada` + `supersededBy?`; inmutable salvo transición a reemplazada.
- **Bitácora**: un archivo por entrada (ULID ordenable → append-only real); `actor`, `eventType` de catálogo cerrado (incluye autorizaciones con qué/quién), `payload` tipado, `refs[]`.
- **Changelog**: `taskRefs[]`, `branch`, `summary`, `changes[]`, `impacts[]?` (lo que los agentes leen tras pull).
- **Configuración**: `artifactLanguage` inmutable validado; `environments[]` mutable (default `dev, staging, prod`); `layers[]`, `providers[]`, `platformVersion`, `schemaVersions`.
- **Declaración de agentes**: `packs[]/agents[]` como `id@versión`; `personalization` (displayName único + sección personalizable); `permissions` (perfil predefinido o mapa explícito de 7 categorías; el mapa prevalece).
- **Metadatos documentales**: front-matter en el propio markdown; el validador lo parsea; el índice SQLite se construye de ahí. Documento inválido = no indexable (rechazado/marcado; edición manual que rompe estructura → rastreo de autor por git + notificación de impacto).
- **Manifiesto del marketplace** (API pública): `manifestVersion`, `platformVersion`; `items[]` `{id, type: agent|skill|pack|flow, name, description {es,en} obligatorios, providers|"all", requiredCapabilities[], files[], checksums, stage/rol}`. Packs por composición.

## 9. Motor de flujos

- **Flujos = definiciones declarativas** (JSON) distribuibles por el marketplace (`type: flow`); el motor genérico las ejecuta. El flujo de inicialización (5 etapas) es la primera definición distribuida; los flujos de F3 son datos nuevos, no código nuevo.
- **Etapa declara**: propósito, participantes (roles), documentos de entrada (vía índice), artefactos de salida con esquema, omitibilidad, datos bloqueantes que resuelve.
- **Progreso versionado** en la carpeta de gestión (retomabilidad de equipo); conversación en curso efímera (local).
- **Máquina de estados por etapa**: `pendiente → en_curso → pendiente_de_cierre → cerrada | omitida`.
- **Extracción al cierre**: el agente produce salida estructurada contra esquema; el core valida; reintento acotado; fallo persistente → `pendiente_de_cierre` + notificación. Aprobación humana de lo extraído configurable por permisos.
- **Datos bloqueantes**: checklist declarado por el flujo; al cierre el motor computa faltantes y pregunta solo los huecos; checklist completo habilita el scaffold (determinístico).
- **Protocolo de turnos** (moderador programático, M2): agenda por etapa, turnos por rondas, prioridad del PO en etapas técnicas, interrupción del usuario siempre disponible, convergencia por declaración estructurada de "sin objeciones". En M1 la misma maquinaria corre con un participante.

## 10. Modelo git

- Rama de integración: `main`. Ramas de trabajo: `{task|bug}/{ulid-corto}-{slug}` (ej. `task/01hx4q-login-oauth`) — enlace inequívoco rama↔tarea, parseable.
- Una tarea = una rama = un worktree = un changelog. Worktrees aislados para todo entregable; la plataforma llega a commit+push (según permisos) y sugiere título/descripción de PR.
- **Artefactos de gestión** (estados, bitácora, progreso): commit directo a la rama activa/integración — sus conflictos son los "conflictos git normales" aceptados. Las ramas de trabajo son para entregables.
- **Init**: commitea directo a `main`; un commit por cierre de etapa (historial auditable).
- Changelog versionado generado al preparar push; tras pull, los agentes lo leen para actualizar backlog y detectar documentos a revisar.
- Limpieza de worktrees/ramas huérfanas: determinística por la plataforma.

## 11. Sync y deriva

1. Materializar registra hash por archivo generado (SQLite, derivable).
2. Cada `sync` (y tras pull): recompilar y comparar — **modificados** (edición manual), **faltantes**, **huérfanos** (sin declaración).
3. Resolución por ítem: `sobrescribir` (restaurar canónico) · `conservar` (advertencia persistente) · `adoptar` (mover la edición a la sección de personalización versionada, si aplica).

## 12. Actualizaciones y compatibilidad

- `upgrade` integrado; changelog de la actualización al panel de notificaciones.
- Sección de personalización nunca se pisa; ediciones fuera de ella → decisión con diff (primera versión: aceptar todo / conservar todo / ver diff).
- Cambio de formato: notificación previa; migración por proyecto, posponible; exige estado limpio (sin worktrees activos, o cierre ordenado).
- **Ventana de retrocompatibilidad**: la versión `N` lee esquemas de `N-1` y `N-2`, escribe siempre en `N`. Proyectos en `N-3` o anterior migran vía versión intermedia (disponible en Releases). Ventana declarada como contrato público.

## 13. Seguridad técnica

- Portal: bind exclusivo a localhost, token de sesión generado por la CLI, ninguna acción destructiva por GET (mitiga CSRF/DNS-rebinding).
- Git y procesos: invocación por arrays de argumentos, nunca shell interpolado.
- Definiciones descargadas: checksums del manifiesto + fuente oficial fijada en configuración. Firma criptográfica diferida a la apertura a terceros (F6).
- Permisos de agentes: 7 categorías × 3 niveles, perfiles conservador/estándar/autónomo, arbitraje en el core, sin acumulación especulativa, autorización manual registrada en bitácora (quién/qué/cuándo).
- CI: `-race` obligatorio; test del invariante de almacenes.

## 14. Catálogo de agentes M1

| Etapa | Rol canónico | Nombre por defecto | Responsabilidad |
|---|---|---|---|
| 1 Exploración | `idea-explorer` | Sofía | Ideación estilo BMAD → visión |
| 2 Definición | `product-owner` | Elena | PRD, épicas, alcance, usuarios |
| 3 Análisis técnico | `architect` | Andrés | Stack, arquitectura conceptual, capas, ambientes (M2: mesa con `ui-ux` y `qa`, Elena presente) |
| 4 Planificación | `project-manager` | Marta | Backlog semilla |

La extracción al cierre la ejecuta el agente de la etapa (mismo contexto). Roles especializados (devs por lenguaje, QA ejecutor, DevOps) se diseñan con los flujos de F3. Nombres ajustables vía capa de personalización.

## 15. Pendientes conscientes (no bloquean M1)

- Validación de marca del nombre Atrio y elección de organización GitHub distintiva antes del primer release público (existe Atrio Inc./atrio.io en el espacio dev-tools — ADR-015).
- Política fina de disparadores de actualización documental (F4).
- Diseño de definiciones de agentes especializados (F3).
- Modelo de confianza/firma para marketplace de terceros (F6).
