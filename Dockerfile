FROM scratch
MAINTAINER Lucas Servén <lserven@gmail.com>
COPY bin/jupyter-operator /
ENTRYPOINT ["/jupyter-operator"]
