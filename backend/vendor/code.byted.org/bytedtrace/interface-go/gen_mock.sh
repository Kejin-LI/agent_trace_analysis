#!/bin/bash

set -e
# mock tracer
mockgen -destination=./mock/mock_tracer.go -package=mock code.byted.org/bytedtrace/interface-go/mock Tracer
# mock span
mockgen -destination=./mock/mock_span.go -package=mock code.byted.org/bytedtrace/interface-go Span
# mock span context
mockgen -destination=./mock/mock_span_context.go -package=mock code.byted.org/bytedtrace/interface-go SpanContext
# mock span handler
mockgen -destination=./mock/mock_span_handler.go -package=mock code.byted.org/bytedtrace/interface-go/mock SpanHandler
# mock event factory
mockgen -destination=./mock/mock_event_factory.go -package=mock code.byted.org/bytedtrace/interface-go EventFactory
# mock event
mockgen -destination=./mock/mock_event.go -package=mock code.byted.org/bytedtrace/interface-go Event

