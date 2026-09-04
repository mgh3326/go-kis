# Safety regression record

The repository tests reject an unapproved REST host, HTTP REST, removal of
the 60-second OAuth safety buffer, and removal of the REST limiter wait. The
read-only AST boundary test rejects account-mutation paths, transaction IDs,
and public mutation symbols in production Go source.
