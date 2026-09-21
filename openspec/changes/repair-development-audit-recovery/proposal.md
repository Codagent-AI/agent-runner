# Repair development audit recovery

Development audits currently lose otherwise valid observations when a model note
contains a slash, and interrupted launches can remain reserved indefinitely.
This change preserves safe categorical observations, makes delivery failures
durable, adds explicit reconciliation for reserved automatic audits, and adds a
temporary two-machine recovery tool.
