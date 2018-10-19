FROM scratch
MAINTAINER squat <lserven@gmail.com>
COPY bin/jupyter-operator /
ENTRYPOINT ["/jupyter-operator"]
