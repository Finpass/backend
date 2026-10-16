# Response and error contract

One envelope for all three repos (backend, ai, frontend). The backend owns this file.

## Success

```json
{ "success": true, "message": "Note created", "data": { } }
```

## Failure

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "Validation failed",
    "details": [{ "field": "title", "message": "required" }]
  },
  "requestId": "a79fb5faae5869f3"
}
```

`details` is always an array. `requestId` mirrors the `X-Request-Id` response header.

## Codes

| Code | HTTP | Use |
|------|------|-----|
| BAD_REQUEST | 400 | malformed JSON, unknown field |
| UNAUTHORIZED | 401 | missing/invalid credentials |
| FORBIDDEN | 403 | authenticated but not allowed |
| NOT_FOUND | 404 | unknown resource or route |
| METHOD_NOT_ALLOWED | 405 | |
| CONFLICT | 409 | duplicate, stale state |
| VALIDATION_FAILED | 422 | field-level input errors (`details`) |
| UNPROCESSABLE | 422 | input is well-formed but a business rule rejects it |
| TOO_MANY_REQUESTS | 429 | |
| INTERNAL | 500 | unexpected; message is always generic, cause only in logs |
| UPSTREAM_FAILED | 502 | a service we call failed |
| UNAVAILABLE | 503 | dependency down (readiness) |
| UPSTREAM_TIMEOUT | 504 | a service we call timed out |

A code is added in all three repos in the same change: `backend/internal/shared/apperr`, `backend/internal/platform/httpx/response.go`, `ai/src/core/errors.py`, `frontend/src/types/api.ts`.

## Where errors are raised and mapped

- domain/application code returns `apperr.Error` (Go) or raises `AppError` (Python). It never knows about HTTP.
- exactly one place per service maps code to status and writes the envelope: `httpx.WriteError` (Go), `core/error_handler.py` (Python).
- 5xx: logged with cause and request id; the client sees a generic message.
- service to service: backend calls ai through `platform/aiclient`. Client faults (4xx) keep their code; ai 5xx/network/timeout become UPSTREAM_FAILED / UPSTREAM_TIMEOUT. `X-Request-Id` and `X-Internal-Api-Key` are forwarded.
- frontend: `lib/http.ts` turns every failure into one `ApiError`; `userMessage` hides 5xx detail and shows the request id.
