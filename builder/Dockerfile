ARG builder_image
FROM $builder_image
RUN apk --update --no-cache --allow-untrusted --repository http://dl-cdn.alpinelinux.org/alpine/v3.16/main add ca-certificates
# RUN apk --no-cache --allow-untrusted --repository http://dl-cdn.alpinelinux.org/alpine/v3.16/main add make git
RUN apk --no-cache add make git

ENTRYPOINT ["/bin/sh"]