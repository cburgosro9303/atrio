# Definición de Producto — Plataforma de Desarrollo Asistido por Agentes

**Documento consolidado de la sesión de descubrimiento**
Estado: Descubrimiento funcional cerrado · Listo para fase de definición técnica y arquitectónica
Enfoque: Agnóstico a tecnología de implementación (salvo restricciones declaradas)

---

## 1. Visión

Plataforma **open source, de ejecución local**, para desarrollo de software asistido por agentes de IA. Su artefacto central es un **monorepo autocontenido**: el repositorio contiene el código de todas las capas del producto (front, back, IaC, etc.), la documentación viva, y el estado completo de gestión del proyecto (tareas, épicas, decisiones, bitácora, configuración de agentes y flujos).

El usuario interactúa mediante dos interfaces con **paridad de capacidades** (CLI y portal web local). La plataforma orquesta **CLIs de agentes de IA ya instaladas en la máquina** (Claude Code, Copilot, Antigravity) mediante agentes especializados por rol que ejecutan flujos del ciclo de vida completo: ideación, creación desde cero, features incrementales, consultas, ceremonias Scrum, revisión y análisis.

### Promesas centrales

1. **Agnosticismo de proveedor de IA**: los flujos no dependen del proveedor concreto; se aprovechan features nativas donde existen y se aplican contramedidas donde no.
2. **Portabilidad total vía repositorio**: quien clona el repo y ejecuta `sync` retoma el proyecto íntegro; lo único externo son los proveedores instalados en la máquina local.
3. **Eficiencia de contexto por diseño**: documentación fragmentada e indexada; los agentes cargan solo lo que su tarea requiere.
4. **Autonomía gobernada**: el usuario decide, por agente y por categoría de acción, qué se ejecuta automáticamente y qué requiere autorización.

---

## 2. Principios rectores

En orden de prioridad:

1. Seguridad
2. Estabilidad
3. Confiabilidad
4. Modularidad
5. Extensibilidad
6. Bajo acoplamiento
7. Alta cohesión
8. Claridad conceptual
9. Facilidad de mantenimiento
10. Evolución futura

Principios operativos adicionales confirmados durante la sesión:

- **Código antes que agentes**: todo lo que pueda resolverse determinísticamente con código no se delega a un agente (índices, validaciones, limpieza de worktrees, materialización de definiciones).
- **Documentación fragmentada e indexada**: múltiples documentos cortos y claros antes que documentos extensos; los agentes consultan un índice y cargan solo lo necesario.
- **Aprovechamiento máximo de proveedores**: agnosticismo no significa mínimo común denominador; features nativas se usan donde existan, con contramedidas (feature equivalente o inyección en contexto/prompt) donde no.
- **Paridad de capacidades, no de experiencias**: toda operación es posible desde CLI y portal, pero cada interfaz la expresa a su manera.

---

## 3. Problemas que resuelve

- Configurar un proyecto multi-capa desde cero es lento y repetitivo.
- Las herramientas de IA actuales atan al usuario a un proveedor específico.
- El conocimiento del proyecto (decisiones, tareas, contexto) vive fuera del repositorio y se pierde o desincroniza.
- La documentación se desactualiza respecto al código.
- Los agentes desperdician contexto cargando información irrelevante.
- La gestión del proyecto está desconectada de donde ocurre el trabajo.
- El onboarding de quien clona un repo no tiene forma simple de retomar el estado real del proyecto.

---

## 4. Modelo de arquitectura conceptual

### 4.1 Instalación y proyectos

- **CLI global** (modelo Angular CLI): una instalación gestiona N proyectos; multiplataforma (Windows, macOS, Linux).
- **Cada proyecto es autosuficiente**: configuración, tooling y estado propios, versionados en su repositorio.
- **Portal web local**: proceso local levantado por la CLI, interfaz visual de gestión.

### 4.2 Proveedores de IA

- La plataforma orquesta **CLIs de agentes instaladas localmente** (Claude Code, Copilot, Antigravity). Credenciales, autenticación y ejecución del modelo las resuelve cada CLI de proveedor.
- **Decisión a nivel de proyecto**: el proyecto declara qué proveedores usa; se asume que todo el equipo tiene acceso a los mismos. Agregar o quitar un proveedor es un cambio del proyecto que impacta capas y configuraciones (flujo formal de "Gestión de proveedores", Fase F2), con reacción de las demás máquinas vía `sync` sugerido tras pull.
- **Adaptador por proveedor**: interfaz agnóstica diseñada desde el día uno contra las convenciones de los tres proveedores objetivo, implementada primero con un solo adaptador (M1).
- **Matriz de capacidades**: cuando una feature es exclusiva de un proveedor, se avisa al usuario y solo se permite instalarla en el proveedor correspondiente; en los demás se evalúa contramedida.

### 4.3 Distribución de agentes, skills y packs

- Las **definiciones canónicas** viven en el repositorio GitHub oficial de la plataforma, versionadas por tag alineado a la versión de la plataforma.
- Se descargan una vez a un **caché global** por versión (con verificación de integridad por checksums y fuente oficial fijada en configuración).
- Cada proyecto **declara** en su configuración versionada qué agentes/packs usa.
- La **materialización** al proyecto (comando `sync`) copia/enlaza desde el caché hacia las carpetas convencionales de cada proveedor (`.claude/`, etc.), solo a nivel de proyecto, nunca a nivel de usuario.
- `sync` detecta y reporta **deriva** entre lo declarado y lo materializado.
- Un **manifiesto/índice** en el repositorio oficial permite al marketplace del portal presentar cards de lo instalable según proveedores del proyecto.

### 4.4 Capa de personalización

- Las definiciones canónicas incluyen una **sección de personalización** delimitada.
- Nombres de agentes, convenciones del equipo y ajustes viven en esa capa, **versionada en el repositorio del proyecto** (todo el equipo ve los mismos identificadores).
- La plataforma provee nombres por defecto; los agentes tienen nombres tipo persona (Cesar, Diana…), renombrables, con unicidad de nombre.
- Configuración de agentes en dos niveles: **general** (agnóstica) y **específica por proveedor**.

### 4.5 Actualizaciones

- Actualización de la plataforma global nunca se anula por un proyecto.
- **Cambios funcionales de definiciones**: se aplican directamente; el changelog se publica en el panel de notificaciones del equipo.
- **Ediciones dentro de la sección de personalización**: nunca se pisan; se actualiza el resto.
- **Ediciones fuera de la sección de personalización**: se solicita decisión al equipo con diff, idealmente con merge selectivo (elegir qué preservar de cada lado; primera versión puede simplificarse a aceptar todo / conservar todo / ver diff).
- **Cambios de formato de artefactos del proyecto**: notificación previa; si el equipo acepta → protocolo de migración (que exige estado limpio: sin worktrees de agentes activos, o los cierra ordenadamente); si rechaza → la migración de *ese proyecto* se pospone. La plataforma mantiene retrocompatibilidad de lectura durante una ventana de versiones.

---

## 5. Modelo de trabajo con git

- **Disciplina de ramas de la plataforma** (no gitflow literal): ramas efímeras por tarea contra una rama de integración, nombradas por convención de la plataforma. Release/hotfix quedan fuera del alcance (el equipo puede usarlos por fuera).
- **Regla atómica**: una tarea = una rama = un worktree = un changelog.
- Todo trabajo de agentes ocurre en **worktrees aislados**; al terminar, la plataforma llega hasta **commit + push** (según permisos) y **sugiere título y descripción del PR**. Crear el PR es responsabilidad del dev en su portal git.
- **Changelog estructurado versionado**: archivo en el repo, generado por la plataforma al preparar el push. Al hacer pull, los agentes lo leen para entender los cambios bajados y actualizar el estado del backlog. Es el puente de sincronización de conocimiento del equipo.
- Conflictos post-PR: los resuelve el dev, o delega el fix al agente autor del cambio.
- **Identidad = configuración git del proyecto** (user.name/email); si no existe, la plataforma bloquea y solicita/setea los datos. *Limitación aceptada y documentada: es atribución, no seguridad — la identidad git es falsificable.*
- Conflictos en artefactos de gestión entre miembros del equipo se resuelven como conflictos git normales. El diseño los minimiza: un archivo por tarea/decisión/entrada; el índice se regenera, nunca se mergea a mano.
- Limpieza de worktrees/ramas huérfanas: gestionada determinísticamente por la plataforma.

---

## 6. Sistema documental

- Documentos **cortos, enfocados y fragmentados**, con metadatos declarados (título, propósito, etiquetas, relaciones).
- **Índice determinístico**: regenerado por la plataforma desde los metadatos (nunca curado por agentes). Los agentes son responsables de declarar bien metadatos y estructura.
- **Documento válido = documento indexable**: validación determinística de estructura/metadatos como paso obligatorio; documentos malformados son rechazados o marcados por la plataforma.
- Los agentes consultan el índice y cargan **solo los documentos que su tarea requiere**.
- Edición manual de artefactos estructurados: válida mientras no rompa la estructura. Si la rompe, la plataforma rastrea el cambio, identifica al autor (atribución git) y notifica el impacto.

### Idiomas

- **Agentes**: conversan en el idioma en que les hable el usuario (espejo dinámico).
- **Interfaz (CLI/portal)**: configurable, cambiable en cualquier momento. Soporta español e inglés.
- **Artefactos y documentos**: idioma definido **al crear el proyecto, inmutable**. Los agentes escriben artefactos en el idioma del proyecto aunque la conversación sea en otro idioma.

---

## 7. Artefactos de gestión

Formatos **estructurados** (no pensados para edición manual), versionados en el repositorio. Tres tipos con contrato diferenciado:

| Artefacto | Naturaleza | Reglas |
|---|---|---|
| **Tareas** | Trabajo con estado | Ciclo de vida completo (sección 8); un archivo por tarea |
| **Decisiones** | Acuerdos con contexto y consecuencias | Inmutables una vez tomadas; solo reemplazables por decisiones nuevas |
| **Bitácora** | Registro cronológico de eventos | Append-only; nadie edita retroactivamente; incluye ejecuciones de agentes, hitos, notas del equipo y registro de autorizaciones |

---

## 8. Ciclo de vida de tareas

El esquema de tarea soporta el ciclo completo desde M1 (aunque M1 solo ejercite los estados iniciales), para evitar migraciones tempranas.

### Fases de estado

1. **Construcción**: la tarea nace en la ideación como base en construcción y evoluciona hasta `ready_for_dev`.
2. **Ejecución**: desarrollo, bloqueo, code review.
   - Una tarea en progreso registra qué agente/humano la tiene.
   - Una tarea bloqueada exige referencia a su bloqueador (otra tarea, pregunta al usuario, decisión pendiente). Las "tareas bloqueantes que resuelve el usuario" son tareas bloqueadas cuyo desbloqueador es una respuesta humana.
3. **Despliegue en bucle por ambiente**: `despliegue {env} → pruebas {env}` para cada ambiente en orden.
4. **Definitivos**: `completada`, `cancelada` — inmutables.

### Reglas

- **Ambientes del proyecto**: se definen en la inicialización; si el usuario no los entrega, se asumen `dev, staging, prod`. Son **mutables** después: agregar un ambiente a mitad del proyecto es posible; las tareas en curso adoptan el nuevo bucle, las cerradas no se tocan.
- **Ambiente de cierre por tarea**: cada tarea define en qué ambiente se considera cerrada (hay tareas que cierran en ambientes bajos mientras otras esperan producción).
- **Estados de despliegue**: siempre los confirma el humano (directamente o instruyendo a un agente que ejecute el cambio de estado en su nombre). Integración con CI/CD para reflejo automático: registrada como evolución futura (F4+).
- **Bugs**: un bug es una tarea de tipo `bug` que **nace en estado `triage`**, con referencia opcional a la tarea origen. Un bug sobre una tarea con estado definitivo referencia la tarea sin reabrirla.

---

## 9. Agentes: roles, permisos y autonomía

### Catálogo de roles (por etapas)

- **Negocio/definición**: exploración de ideas, product owner, analistas de producto.
- **Técnicos**: arquitectura, devs especializados por lenguaje, UI/UX, QA, DevOps (especializados por cloud: AWS, GCP, Azure, o genérico Terraform — Fase F4).
- **Gestión**: agente PM con asignación secuencial/paralela de tareas (F3).

### Sistema de permisos

Siete categorías de acción permisionables por agente, cada una con nivel `permitida | requiere_autorización | denegada`:

1. Leer archivos del proyecto
2. Escribir archivos en su worktree
3. Ejecutar comandos de solo lectura (build, test, lint)
4. Ejecutar comandos arbitrarios
5. Operaciones git locales (commit)
6. Operaciones git remotas (push/pull)
7. Acceso a red más allá del proveedor IA

- **Perfiles predefinidos** (conservador / estándar / autónomo) para evitar configurar 7 switches por agente.
- Permisos **gestionables en cualquier momento**, no rígidos.
- Un agente sin permiso sugiere el comando y espera autorización o ejecución manual (modo pregunta / modo automático).
- **Sin acumulación especulativa**: un agente en modo pregunta solicita autorizaciones en el momento en que las necesita y queda en espera; no ejecuta trabajo alternativo que dependa del resultado no autorizado.
- **Auditoría de autorizaciones**: toda acción autorizada manualmente registra en bitácora quién autorizó, qué y cuándo.

---

## 10. Modelo de conversaciones

- **Chats colaborativos por etapa** en el flujo de inicialización (y reutilizados en ceremonias y flujos de implementación). El protocolo de turnos es gobernado por código, no por los agentes.
- **Chats individuales por agente**: canal paralelo siempre disponible para consultas puntuales.
- **Conversaciones efímeras por defecto**, con persistencia opt-in: conversación completa o "persistir desde este mensaje" (selección del mensaje inicial vía modal de confirmación). Lo persistido entra al corpus documental (indexado).
- **Extracción obligatoria al cierre**: antes de descartar una conversación de flujo/mesa, el sistema exige extraer decisiones → artefacto de decisiones; acuerdos de trabajo → tareas; eventos → bitácora. Si no puede completarse, la sesión queda "pendiente de cierre" y se notifica.

---

## 11. Flujo de inicialización (núcleo del MVP)

`init <nombre>` crea la carpeta, la estructura de trabajo y lo necesario para el portal de gestión. Luego:

1. Pregunta si el usuario aporta un **documento de definición** (del que se extrae lo identificable: lenguajes, tecnologías, definiciones existentes) o parte de cero.
2. De cero: elige continuar en **CLI** (preguntas secuenciales) o **portal** (flujo visual) — portal disponible desde M2.

### Etapas (omitibles, con avance visible y artefacto de salida)

| # | Etapa | Artefacto de salida | Participantes |
|---|---|---|---|
| 1 | Exploración/ideación | Visión del producto | Agentes de negocio (estilo BMAD: construye el PRD si la idea no está clara) |
| 2 | Definición de producto | PRD (épicas, alcance, usuarios) | Agentes de negocio |
| 3 | Análisis técnico | Stack, arquitectura conceptual, capas, **ambientes** | Arquitectura, desarrollo, UI/UX, QA — siempre interactuando con el PO |
| 4 | Planificación inicial | Backlog semilla | PM/PO |
| 5 | Scaffold | Estructura + docs + configuración de gestión | Plataforma (determinístico) |

### Reglas del flujo

- **Mínimo bloqueante para el scaffold**: descripción/propósito, capas del sistema, lenguaje(s)/stack por capa, tipo de aplicación por capa. El lenguaje puede emerger de la exploración; sigue siendo bloqueante para crear carpetas de código.
- Si el usuario **omite etapas**, los datos bloqueantes faltantes se consultan directamente al cierre.
- El scaffold crea **solo estructura + documentación + gestión**; la creación de código base compilable por capa es la **primera tarea del backlog**, no parte del cierre.
- **Reanudabilidad**: cada etapa cerrada persiste su artefacto; la etapa en curso se pierde (conversación efímera) salvo lo extraído. `init` sobre un proyecto a medias detecta el estado y ofrece reanudar.

---

## 12. Seguridad

Prioridad #1 del proyecto. Requisitos confirmados:

1. **Blindaje del portal local** (requisito de M2): bind exclusivo a localhost; token de sesión generado por la CLI al levantar el portal (mitiga CSRF/DNS-rebinding contra el puerto local); ninguna acción destructiva ejecutable por GET.
2. **Integridad de definiciones descargadas**: checksums en el manifiesto y fuente oficial fijada en configuración, desde la primera versión. Firma criptográfica completa diferida a la apertura del ecosistema de terceros.
3. **Permisos de agentes** como control central de acciones sobre filesystem, comandos y git (sección 9).
4. **Cadena de auditoría**: bitácora append-only + changelogs versionados + atribución git + registro de autorizaciones manuales.
5. **Riesgo registrado para el futuro**: si el marketplace se abre a definiciones de terceros (F6), estas equivalen a ejecución de instrucciones no confiables (prompt injection, exfiltración) y requerirán modelo de confianza/firma antes de habilitarse.

---

## 13. Roadmap por fases

### M1 — MVP: inicialización completa por CLI (agente único por etapa)

E1 init · E2 flujo de etapas · E3 sistema documental + índice · E4 caché global + declaración + sync · E5 permisos · E6 worktrees + changelog versionado · E7 artefactos de gestión · E8 definición bloqueante de stack · E9 primer adaptador de proveedor (interfaz agnóstica) · E10 identidad git · Notificaciones mínimas por salida CLI.

### M2 — Portal

R1 portal local (flujo visual de init, chats por etapa, avance) · R2 board (épicas, tareas, estados, actividad de agentes) · R3 mesas colaborativas multi-agente (reemplazan al agente único por etapa; cierre funcional idéntico a M1) · R4 panel de notificaciones · O6 persistencia selectiva de conversaciones · Blindaje del portal (sección 12).

### F2 — Multi-proveedor y ecosistema

R5 marketplace visual (cards, índice, sincronización entre proveedores, avisos de exclusividad) · R6 segundo y tercer adaptador + contramedidas + matriz de capacidades · R7 actualizaciones completas (personalización protegida, diff/merge, protocolo de migración) · R8 reporte de tokens por sesión (aproximado, por proveedor, no persistido) · Flujo de gestión de proveedores del proyecto.

### F3 — Ciclo de vida completo

O1 features sobre lo existente · O2 agente PM (asignación secuencial/paralela) · O3 ceremonias Scrum (moderador humano comparte pantalla, opera el chat de ceremonia; refinamiento → documenta tareas, planning → crea tareas) · O4 revisión/análisis con alertas · O5 consultas Q&A sobre el corpus.

### F4+ — Madurez

F1 herramientas de optimización de contexto (evaluar con métricas reales; E3 ya entrega la ganancia fundacional) · F2 actualización de documentación con política de disparadores (al cerrar tarea, al preparar push — no "cada cambio" literal, pendiente de definición fina) · F3 agentes DevOps por cloud · F4 notificaciones externas · F5 board creativo/interactivo avanzado · F6 ecosistema comunitario del marketplace (con modelo de confianza) · Integración CI/CD para reflejo automático de estados de despliegue.

---

## 14. Escenarios límite resueltos

| Escenario | Comportamiento |
|---|---|
| Clonar con proveedores distintos a los del proyecto | Los proveedores son decisión del proyecto; se asume acceso uniforme del equipo. Cambiarlos es un flujo formal (F2) que ajusta capas y configuraciones |
| Interrupción a mitad del flujo de init | Etapas cerradas persisten; etapa en curso se pierde salvo lo extraído; `init` detecta y ofrece reanudar |
| Dos agentes tocan el mismo artefacto de gestión | Un archivo por tarea/decisión/entrada minimiza colisiones; el índice se regenera, nunca se mergea |
| Edición manual rompe un artefacto estructurado | La plataforma rastrea el cambio, identifica al autor y notifica el impacto |
| Update de plataforma con trabajo en curso | La migración exige estado limpio (sin worktrees activos) o los cierra ordenadamente |
| Bug sobre tarea cerrada | Se crea tarea tipo `bug` en `triage` referenciando la tarea, sin reabrirla |

---

## 15. Limitaciones y riesgos aceptados

1. **Identidad git falsificable**: la atribución no es seguridad. Aceptado como limitación documentada del modelo local/open source.
2. **Conflictos git en artefactos de gestión**: resueltos como conflictos normales, sin mecanismo adicional de coordinación de equipo. Mitigado por diseño de formato.
3. **Abstracción multi-proveedor diseñada con un solo adaptador inicial**: riesgo de abstracción equivocada; mitigado diseñando contra las convenciones de los tres proveedores objetivo.
4. **MVP amplio**: incluye el flujo completo de ideación→análisis→scaffold. Mitigado con la secuenciación M1 (agente único por etapa) → M2 (mesas), con cierre funcional idéntico.
5. **Contramedidas por features faltantes pueden degradar calidad** de forma no evidente; mitigado con matriz de capacidades visible al usuario.
6. **Estados de despliegue reflejan hechos externos**: dependen de confirmación humana; el board puede desfasarse si el humano no actualiza. O4 (alertas) y la futura integración CI/CD lo mitigan.

---

## 16. Preguntas diferidas a la fase técnica

Material de definición posterior, no funcional (todas registradas, ninguna bloquea el diseño funcional):

1. Formato concreto del manifiesto/índice del repositorio de definiciones (API pública del ecosistema open source — alto costo de cambio, diseñar con cuidado).
2. Convención de nombres de ramas de la plataforma.
3. Esquemas concretos de los artefactos estructurados (tarea, decisión, bitácora, changelog, metadatos documentales).
4. Catálogo inicial detallado de agentes por etapa y sus definiciones.
5. Protocolo de turnos de las mesas colaborativas (M2).
6. Política fina de disparadores de actualización documental (F4).
7. Ventana concreta de retrocompatibilidad de lectura entre versiones.
8. Mecánica de detección de deriva de `sync`.
9. Elección del primer proveedor a soportar en M1.
10. Tecnologías de implementación de CLI y portal (explícitamente diferidas durante toda la sesión).

---

## 17. Criterio de finalización — verificación

- ✅ Visión del producto consistente y sin contradicciones relevantes (7 tensiones detectadas y resueltas: T1–T12; 3 inconsistencias cerradas: I1–I3).
- ✅ Principales escenarios funcionales cubiertos (flujo de init completo, trabajo de agentes, actualizaciones, equipo, escenarios límite L1–L5).
- ✅ Reglas de negocio claras (RN1 extendida con ciclo de vida completo, RN2–RN6, política de idiomas, contrato de artefactos).
- ✅ Base sólida para la definición técnica y arquitectónica (sección 16 lista el trabajo pendiente, acotado y no funcional).

**La sesión de descubrimiento queda cerrada.**
