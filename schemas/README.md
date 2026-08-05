# Esquemas de artefactos de Atrio

Contrato público de los artefactos de gestión (ADR-005). Nueve archivos: una definición común y los ocho artefactos que T-010 congela.

| Archivo | Artefacto | Dónde vive en un proyecto |
|---|---|---|
| `common.schema.json` | Envolvente y tipos compartidos | — (solo `$defs`) |
| `task.schema.json` | Tarea y bug | `.atrio/management/tasks/{ulid}.json` |
| `decision.schema.json` | Decisión | `.atrio/management/decisions/{ulid}.json` |
| `log-entry.schema.json` | Entrada de bitácora | `.atrio/management/log/{ulid}.json` |
| `changelog.schema.json` | Changelog | `.atrio/management/changelogs/{ulid}.json` |
| `flow-progress.schema.json` | Progreso de flujo | `.atrio/management/flows/{ulid}.json` |
| `project-config.schema.json` | Configuración de proyecto | `.atrio/config.json` |
| `agents.schema.json` | Declaración, personalización y permisos | `.atrio/agents.json` |
| `document-front-matter.schema.json` | Front-matter documental (forma ya parseada) | cabecera YAML de cada `docs/**/*.md` |

## Convenciones

- **Draft 2020-12.** No es una preferencia: la envolvente se compone con `allOf` y cada artefacto se cierra con `unevaluatedProperties: false`, que es la combinación que draft-07 no permite.
- **`$id` relativo**, idéntico al nombre del archivo, y `$ref` relativos entre esquemas. Atrio todavía no tiene dominio propio (ADR-015 deja abierta la organización), y un `$id` absoluto sería una URL que el proyecto no controla. Publicarlos bajo una base (T-082) no exige tocar los archivos.
- **`schemaVersion` entero** y monótono por tipo de artefacto: la versión `N` lee `N-1` y `N-2` y siempre escribe `N` (`03-arquitectura.md`, §12). `config.json` lleva además el mapa `schemaVersions` para migraciones selectivas.
- **Identificadores y valores de enum en inglés.** El contrato es código; el idioma del proyecto (`artifactLanguage`) gobierna lo que un equipo escribe, y traducir etiquetas es cosa de presentación.
- **Ningún campo desconocido pasa.** Un campo no declarado es un typo en un campo que sí importa. La única puerta abierta a propósito es `personalization.providerSettings` de `agents.json`: la forma de los ajustes específicos de cada proveedor la conoce su adaptador, no este contrato.
- **Un solo sitio por concepto.** El `id@version` del caché, las siete categorías de permisos y el patrón de rama viven en `common.schema.json` y los demás esquemas los referencian. Las categorías aparecen en dos formas —claves del mapa en `agents.json`, valor de enum en la bitácora— y un test las mantiene sincronizadas.
- **Dos envolventes.** Los artefactos JSON llevan la completa (`schemaVersion`, `id`, `createdAt`, `updatedAt`, `createdBy`); el front-matter lleva la reducida (`schemaVersion`, `id`), porque un markdown se edita a mano y las fechas y la autoría ya las sabe git.

## Qué valida el esquema y qué valida el código

Un esquema ve un documento aislado. Las reglas que comparan versiones o cruzan artefactos no caben ahí, y fingir que sí es peor que declararlo:

| Regla | Dónde |
|---|---|
| `blockedBy` obligatorio en una tarea bloqueada | esquema (`task`) |
| El bucle por ambiente nombra su ambiente | esquema (`task`) |
| Solo un bug nace en `triage` y solo un bug viene de otra tarea | esquema (`task`) |
| Un despliegue lo confirma una persona, nunca un agente | esquema (`task`) |
| Una decisión reemplazada dice por cuál | esquema (`decision`) |
| Una autorización registra qué, a qué agente y quién la concedió | esquema (`log-entry`) |
| Un renombrado dice de dónde viene | esquema (`changelog`) |
| Una etapa cerrada persiste su artefacto; una omitida, su motivo | esquema (`flow-progress`) |
| Un bloque de permisos declara perfil o mapa, y el mapa decide las 7 categorías | esquema (`agents`) |
| Estados definitivos (`completed`, `cancelled`) inmutables | código — compara la versión anterior con la nueva (T-040) |
| Decisión inmutable salvo la transición a `superseded` | código (T-040) |
| `artifactLanguage` inmutable tras la creación | código (T-020) |
| Unicidad de `displayName` entre agentes | código (T-020/T-041) |
| Unicidad del `id` de etapa dentro de un progreso de flujo | código (T-070) — `uniqueItems` no sirve: dos etapas con el mismo `id` y distinto contenido lo pasarían |
| Bitácora append-only | código — es regla del almacén, no del documento (T-020) |
| `closureEnvironment` ∈ `environments` del proyecto | código — cruza dos artefactos (T-040) |
| Los `refs`/`target` apuntan a artefactos que existen | código (T-020, T-022) |
| Orden legal de transiciones de estado | código (T-040, T-070) |
| Expansión perfil → mapa de permisos | código (T-041) |

## Pruebas

`go test ./schemas/` compila cada esquema contra el meta-esquema, comprueba las convenciones de arriba y ejerce cada uno contra sus fixtures de `testdata/`. Una fixture inválida se llama `<propiedad>--<motivo>.json`, y el test no se conforma con que sea rechazada: exige que el error del validador **apunte a esa propiedad**, recorriendo el árbol de causas. Un rechazo que no sabe decir qué campo está mal no es el error reparador que la plataforma promete.
