FROM alpine:latest

WORKDIR /app
COPY prostagma .

ENV PROSTAGMA_HOST=
ENV PROSTAGMA_SECRET=

# Client only
ENV PROSTAGMA_TRIGGER=
ENV PROSTAGMA_SCRIPT=

RUN apk add --update aws-cli

CMD ["/app/prostagma"]