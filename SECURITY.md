# Security policy

Please report security issues privately through GitHub Security Advisories. Do not include controller credentials, agent tokens, WebDAV credentials, or backup archives in a public issue.

The controller must be served over HTTPS. Keep `data/master.key`, `data/state.json`, the bootstrap secret, and the administrator password outside source control and include them in the controller's own disaster-recovery plan.
