FROM alpine:latest

WORKDIR /app
COPY prostagma .

ENV PROSTAGMA_HOST=
ENV PROSTAGMA_SECRET=
ENV PROSTAGMA_SHELL=/bin/sh
ENV PROSTAGMA_AWS=/usr/bin/aws

# Client only
ENV PROSTAGMA_TRIGGER=
ENV PROSTAGMA_SCRIPT=

RUN apk add --update aws-cli

CMD ["/app/prostagma"]