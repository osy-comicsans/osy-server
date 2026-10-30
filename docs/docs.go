package docs

import "github.com/swaggo/swag"

const docTemplate = `{
  "swagger": "2.0",
  "info": {
    "title": "OSYN API",
    "description": "OSYN ingests Africa's Talking SMS messages through a transactional inbox/outbox pipeline and exposes operational APIs. Operational endpoints require an API key supplied through X-API-Key or Authorization: Bearer <key>.",
    "version": "1.0.0"
  },
  "host": "localhost:8080",
  "basePath": "/",
  "schemes": ["http"],
  "consumes": ["application/json"],
  "produces": ["application/json"],
  "securityDefinitions": {
    "ApiKeyAuth": {
      "type": "apiKey",
      "name": "X-API-Key",
      "in": "header",
      "description": "API key configured through API_KEY."
    },
    "BearerAuth": {
      "type": "apiKey",
      "name": "Authorization",
      "in": "header",
      "description": "Bearer token containing the configured API key."
    }
  },
  "paths": {
    "/health": {
      "get": {
        "tags": ["System"],
        "summary": "Liveness check",
        "operationId": "health",
        "responses": {
          "200": {"description": "Process is alive", "schema": {"$ref": "#/definitions/Health"}}
        }
      }
    },
    "/ready": {
      "get": {
        "tags": ["System"],
        "summary": "Readiness check",
        "description": "Checks PostgreSQL and NATS connectivity.",
        "operationId": "ready",
        "responses": {
          "200": {"description": "All dependencies are ready", "schema": {"$ref": "#/definitions/Readiness"}},
          "503": {"description": "One or more dependencies are unavailable", "schema": {"$ref": "#/definitions/Readiness"}}
        }
      }
    },
    "/api/v1/messages": {
      "get": {
        "tags": ["Messages"],
        "summary": "List SMS messages",
        "operationId": "listMessages",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "page", "in": "query", "type": "integer", "minimum": 1, "default": 1},
          {"name": "page_size", "in": "query", "type": "integer", "minimum": 1, "maximum": 100, "default": 20},
          {"name": "sender", "in": "query", "type": "string", "description": "Exact sender match."},
          {"name": "from", "in": "query", "type": "string", "description": "Inclusive lower bound as RFC3339 or YYYY-MM-DD."},
          {"name": "to", "in": "query", "type": "string", "description": "Inclusive upper bound as RFC3339 or YYYY-MM-DD."},
          {"name": "message_id", "in": "query", "type": "string", "description": "Internal ID or provider message ID."}
        ],
        "responses": {
          "200": {"description": "Paginated messages", "schema": {"$ref": "#/definitions/MessagePage"}},
          "400": {"description": "Invalid filter", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/messages/{id}": {
      "get": {
        "tags": ["Messages"],
        "summary": "Get an SMS message",
        "operationId": "getMessage",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "integer", "format": "int64"}
        ],
        "responses": {
          "200": {"description": "SMS message", "schema": {"$ref": "#/definitions/Message"}},
          "400": {"description": "Invalid ID", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "404": {"description": "Message not found", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/stats": {
      "get": {
        "tags": ["Operations"],
        "summary": "Get platform statistics",
        "operationId": "stats",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "responses": {
          "200": {"description": "Message, outbox, and DLQ counts", "schema": {"$ref": "#/definitions/Stats"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/outbox": {
      "get": {
        "tags": ["Outbox"],
        "summary": "List outbox events",
        "operationId": "listOutbox",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "page", "in": "query", "type": "integer", "minimum": 1, "default": 1},
          {"name": "page_size", "in": "query", "type": "integer", "minimum": 1, "maximum": 100, "default": 20},
          {"name": "status", "in": "query", "type": "string", "enum": ["pending", "processing", "published", "failed"]}
        ],
        "responses": {
          "200": {"description": "Paginated outbox events", "schema": {"$ref": "#/definitions/OutboxPage"}},
          "400": {"description": "Invalid status", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/outbox/{id}": {
      "get": {
        "tags": ["Outbox"],
        "summary": "Get an outbox event",
        "operationId": "getOutbox",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string", "format": "uuid"}
        ],
        "responses": {
          "200": {"description": "Outbox event", "schema": {"$ref": "#/definitions/OutboxEvent"}},
          "400": {"description": "Invalid ID", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "404": {"description": "Outbox event not found", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/dlq": {
      "get": {
        "tags": ["Dead Letters"],
        "summary": "List dead letter messages",
        "operationId": "listDeadLetters",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "page", "in": "query", "type": "integer", "minimum": 1, "default": 1},
          {"name": "page_size", "in": "query", "type": "integer", "minimum": 1, "maximum": 100, "default": 20},
          {"name": "status", "in": "query", "type": "string", "enum": ["unresolved", "resolved"]}
        ],
        "responses": {
          "200": {"description": "Paginated dead letters", "schema": {"$ref": "#/definitions/DeadLetterPage"}},
          "400": {"description": "Invalid status", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/dlq/{id}": {
      "get": {
        "tags": ["Dead Letters"],
        "summary": "Get a dead letter",
        "operationId": "getDeadLetter",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string", "format": "uuid"}
        ],
        "responses": {
          "200": {"description": "Dead letter", "schema": {"$ref": "#/definitions/DeadLetter"}},
          "400": {"description": "Invalid ID", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "404": {"description": "Dead letter not found", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/dlq/{id}/replay": {
      "post": {
        "tags": ["Dead Letters"],
        "summary": "Replay a dead letter",
        "description": "Atomically creates a new outbox event with a new event ID and replay_of set to the original event ID. The publisher sends it to the original sms.incoming subject. The dead letter remains available and keeps its replay count and last replay error.",
        "operationId": "replayDeadLetter",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string", "format": "uuid"}
        ],
        "responses": {
          "202": {"description": "Replay event queued", "schema": {"$ref": "#/definitions/ReplayResponse"}},
          "400": {"description": "Invalid ID or payload", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "404": {"description": "Dead letter not found", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "409": {"description": "Dead letter is resolved", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/dlq/{id}/resolve": {
      "post": {
        "tags": ["Dead Letters"],
        "summary": "Resolve a dead letter",
        "operationId": "resolveDeadLetter",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string", "format": "uuid"}
        ],
        "responses": {
          "200": {"description": "Resolved dead letter", "schema": {"$ref": "#/definitions/DeadLetter"}},
          "400": {"description": "Invalid ID", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "404": {"description": "Dead letter not found", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    },
    "/api/v1/metrics": {
      "get": {
        "tags": ["Operations"],
        "summary": "Get operational metrics",
        "operationId": "metrics",
        "security": [{"ApiKeyAuth": []}, {"BearerAuth": []}],
        "responses": {
          "200": {"description": "Runtime, message, outbox, and DLQ metrics", "schema": {"$ref": "#/definitions/Metrics"}},
          "401": {"description": "Missing or invalid API key", "schema": {"$ref": "#/definitions/ErrorEnvelope"}},
          "500": {"description": "Internal error", "schema": {"$ref": "#/definitions/ErrorEnvelope"}}
        }
      }
    }
  },
  "definitions": {
    "Health": {
      "type": "object",
      "properties": {"status": {"type": "string", "example": "ok"}}
    },
    "Readiness": {
      "type": "object",
      "properties": {
        "status": {"type": "string", "example": "ready"},
        "checks": {
          "type": "object",
          "additionalProperties": {"type": "string"},
          "example": {"postgres": "ok", "nats": "ok"}
        }
      }
    },
    "ErrorEnvelope": {
      "type": "object",
      "properties": {"error": {"$ref": "#/definitions/ErrorBody"}}
    },
    "ErrorBody": {
      "type": "object",
      "properties": {
        "code": {"type": "string", "example": "not_found"},
        "message": {"type": "string", "example": "message not found"}
      }
    },
    "Message": {
      "type": "object",
      "properties": {
        "id": {"type": "integer", "format": "int64"},
        "provider": {"type": "string"},
        "provider_message_id": {"type": "string"},
        "sender": {"type": "string"},
        "recipient": {"type": "string"},
        "message": {"type": "string"},
        "received_at": {"type": "string", "format": "date-time"},
        "processing_status": {"type": "string", "enum": ["received", "processed", "failed"]},
        "processed_at": {"type": "string", "format": "date-time"},
        "processing_error": {"type": "string"},
        "created_at": {"type": "string", "format": "date-time"}
      }
    },
    "MessagePage": {
      "type": "object",
      "properties": {
        "data": {"type": "array", "items": {"$ref": "#/definitions/Message"}},
        "page": {"type": "integer"},
        "page_size": {"type": "integer"},
        "total": {"type": "integer", "format": "int64"},
        "total_pages": {"type": "integer"}
      }
    },
    "OutboxEvent": {
      "type": "object",
      "properties": {
        "id": {"type": "string", "format": "uuid"},
        "aggregate_id": {"type": "string"},
        "event_type": {"type": "string"},
        "subject": {"type": "string"},
        "payload": {"type": "object"},
        "status": {"type": "string", "enum": ["pending", "processing", "published", "failed"]},
        "attempts": {"type": "integer"},
        "next_attempt_at": {"type": "string", "format": "date-time"},
        "published_at": {"type": "string", "format": "date-time"},
        "last_error": {"type": "string"},
        "created_at": {"type": "string", "format": "date-time"},
        "updated_at": {"type": "string", "format": "date-time"}
      }
    },
    "OutboxPage": {
      "type": "object",
      "properties": {
        "data": {"type": "array", "items": {"$ref": "#/definitions/OutboxEvent"}},
        "page": {"type": "integer"},
        "page_size": {"type": "integer"},
        "total": {"type": "integer", "format": "int64"},
        "total_pages": {"type": "integer"}
      }
    },
    "DeadLetter": {
      "type": "object",
      "properties": {
        "id": {"type": "string", "format": "uuid"},
        "original_message_id": {"type": "integer", "format": "int64"},
        "event_id": {"type": "string"},
        "payload": {"type": "object"},
        "error": {"type": "string"},
        "delivery_attempts": {"type": "integer"},
        "status": {"type": "string", "enum": ["unresolved", "resolved"]},
        "replay_count": {"type": "integer"},
        "last_replay_error": {"type": "string"},
        "last_replayed_at": {"type": "string", "format": "date-time"},
        "created_at": {"type": "string", "format": "date-time"},
        "resolved_at": {"type": "string", "format": "date-time"}
      }
    },
    "DeadLetterPage": {
      "type": "object",
      "properties": {
        "data": {"type": "array", "items": {"$ref": "#/definitions/DeadLetter"}},
        "page": {"type": "integer"},
        "page_size": {"type": "integer"},
        "total": {"type": "integer", "format": "int64"},
        "total_pages": {"type": "integer"}
      }
    },
    "ReplayResponse": {
      "type": "object",
      "properties": {
        "status": {"type": "string", "example": "queued"},
        "event_id": {"type": "string", "format": "uuid"}
      }
    },
    "Stats": {
      "type": "object",
      "properties": {
        "total_messages": {"type": "integer", "format": "int64"},
        "processed_messages": {"type": "integer", "format": "int64"},
        "pending_outbox_events": {"type": "integer", "format": "int64"},
        "failed_outbox_events": {"type": "integer", "format": "int64"},
        "dlq_count": {"type": "integer", "format": "int64"}
      }
    },
    "Metrics": {
      "type": "object",
      "properties": {
        "uptime_seconds": {"type": "integer", "format": "int64"},
        "requests_total": {"type": "integer", "format": "int64"},
        "requests_failed": {"type": "integer", "format": "int64"},
        "requests_in_flight": {"type": "integer", "format": "int64"},
        "goroutines": {"type": "integer"},
        "messages": {
          "type": "object",
          "properties": {
            "total": {"type": "integer", "format": "int64"},
            "received": {"type": "integer", "format": "int64"},
            "processed": {"type": "integer", "format": "int64"},
            "failed": {"type": "integer", "format": "int64"}
          }
        },
        "outbox": {
          "type": "object",
          "properties": {
            "pending": {"type": "integer", "format": "int64"},
            "processing": {"type": "integer", "format": "int64"},
            "published": {"type": "integer", "format": "int64"},
            "failed": {"type": "integer", "format": "int64"}
          }
        },
        "dlq": {
          "type": "object",
          "properties": {
            "unresolved": {"type": "integer", "format": "int64"},
            "resolved": {"type": "integer", "format": "int64"}
          }
        }
      }
    }
  }
}
`

var SwaggerInfo = &swag.Spec{
	Version:          "1.0.0",
	Host:             "localhost:8080",
	BasePath:         "/",
	Schemes:          []string{"http"},
	Title:            "OSYN API",
	Description:      "OSYN SMS ingestion and operations API.",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
}

type swaggerSpec struct{}

func (s *swaggerSpec) ReadDoc() string {
	return docTemplate
}

func init() {
	swag.Register(swag.Name, &swaggerSpec{})
}
