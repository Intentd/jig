FROM busybox:1.37
ARG GREETING=hello
RUN mkdir /www && echo "${GREETING} from jig" > /www/index.html
EXPOSE 8080
CMD ["httpd", "-f", "-p", "8080", "-h", "/www"]
