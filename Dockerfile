FROM golang:1.14-alpine3.11 as build

# add unprivileged user
RUN adduser -s /bin/true -u 1000 -D -h /app app \
  && sed -i -r "/^(app|root)/!d" /etc/group /etc/passwd \
  && sed -i -r 's#^(.*):[^:]*$#\1:/sbin/nologin#' /etc/passwd

WORKDIR /app
COPY . .

RUN chmod +x build.sh && chown app:app -R /app

USER app
RUN /app/build.sh

# Start with empty image
FROM scratch as runtime

WORKDIR /app
COPY --from=build /app/bin/healthcheck /app/healthcheck

# add-in our unprivileged user
COPY --from=build /etc/passwd /etc/group /etc/shadow /etc/

EXPOSE 8080

USER app
ENTRYPOINT ["/app/healthcheck", "8080", "5", "http://localhost:8080/self"]
