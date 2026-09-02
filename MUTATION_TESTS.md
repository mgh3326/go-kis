# Mutation-regression record

The following mutants were reproduced locally and restored before this change:

| Mutant | Guard | Expected failure |
|---|---|---|
| Follow HTTP redirects | `TestRedirectBlocked` | redirect is returned as `kis.ErrRedirectBlocked` |
| Render the configured app secret in an API error | `TestAPIErrorRedactsSecret` | secret cannot occur in `error.Error()` |
| Insert a default host | `TestHostRequired` | construction returns `kis.ErrHostRequired` |
| Omit a mutation hashkey | `TestMutationWithoutHashKeyIsRejected` | request returns `kis.ErrHashKeyRequired` |
