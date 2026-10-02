# Web capabilities

`cyber-web` is the profile-neutral Hub. Its core owns authentication, nodes,
sessions, AOP events, configuration and artifacts. Product features are mounted
as server capabilities and describe themselves through `GET /api/manifest`.
The same manifest lists installable external node profiles, so Quick Connect
does not hard-code the scan and audit buttons.

A capability owns its schema modules, routes, AOP namespace handlers and close
lifecycle. The Web service only invokes those hooks; it does not import a new
capability's protobufs. The existing scan console remains a compatibility path
until its storage and API implementation are moved behind the same seam.
`cyber-web` starts with the core capability only.

Profile nodes add their capability ids to the AOP enrollment hello. The browser
unions those ids with the Hub manifest and mounts matching built-in frontend
plugins. Plugins contribute views and protocol registrations through a disposer-
backed runtime, so unloading a profile removes its effects.

The scan profile remains available through the existing `aiscan web` entrypoint
while the generic `cyber-web` binary stays scan-free. Both entrypoints publish
the scan/audit node profile descriptors; a future distribution can replace
those descriptors with its own profile IDs. New domain features should follow
the same server capability and frontend plugin boundaries.
