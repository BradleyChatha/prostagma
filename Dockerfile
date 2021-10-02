FROM dlangchina/dlang-dmd AS BUILD
COPY . .
RUN apt-get update && apt-get -y upgrade && apt-get -y install libssl-dev && dub build -b release

FROM debian:latest
COPY --from=BUILD prostagma ./prostagma

ENV PROSTAGMA_HOST=0.0.0.0
ENV PROSTAGMA_PORT=8080
ENV PROSTAGMA_HTTP_METHOD=http
ENV PROSTAGMA_SECRET=
ENV AWS_ACCESS_KEY_ID=
ENV AWS_SECRET_ACCESS_KEY=
ENV AWS_DEFAULT_REGION=

CMD [ "./prostagma", "server" ]