# Jupyter-Operator

This is a Kubernetes operator for [Jupyter Notebooks](https://jupyter.org/).

[![Build Status](https://travis-ci.org/squat/jupyter-operator.svg?branch=master)](https://travis-ci.org/squat/jupyter-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/squat/jupyter-operator)](https://goreportcard.com/report/github.com/squat/jupyter-operator)

## Overview

The Jupyter Operator automates the deployment of Jupyter Notebooks to a Kubernetes Cluster.
It configures TLS certificates for the Notebook server and exposes the application via ingress and service resources.

## Requirements

* Kubernetes v1.7+

## Usage

### Deploy the Jupyter Operator

```sh
kubectl create -f examples/deployment.yaml
```

### Create a Notebook

```sh
kubectl create -f examples/notebook.yaml
```

### Access the Notebook

Resolve DNS for `example-notebook.example.com` as the Kubernetes cluster, e.g. edit `/etc/hosts`:
```sh
<kubernetes-ip-address> example-notebook.example.com
```

Navigate a browser to `example-notebook.example.com` and login with the password `mypassword`.
