# Introduction

Mostly for my own reference and to help others that run into the same problem, this is an example of how one can handle aggregated healthchecks in kubernetes when your app has a dependency on one or more sidecars or containers in a pod, for example in the case of using Istio service mesh and it's sidecar proxy, and you don't want your application container to start receiving traffic until all of these are ready.  I've seen a lot of issues/requests around this, and using aggregated health checks was one of the recommended solutions.

**Disclaimer**: I've never done GO before, so the code is a bit messy and likely not even good... but it works! ;)

**Note**: Hopefully we will soon see [native support](https://github.com/kubernetes/enhancements/issues/753) for sidecar containers.

## Requirements

- One of the prerequisites for this solution is that CURL (and similar tooling) is not available on the system due to image hardening (i.e. security reasons).  
- The other is that the solution must be as small and resource-efficient as possible (without being too complicated, hence GO).  In my tests, the container uses 1m CPU and 8Mib memory with the default healthcheck frequency.

## Related Issues/Information

Here's some links to related issues and information where the topic is discussed:

- [Kubernetes Enhancement #753](https://github.com/kubernetes/enhancements/issues/753) - Sidecar Containers (maybe we'll get this soon?!)
- [Kubernetes Issue #65502](https://github.com/kubernetes/kubernetes/issues/65502) - Support startup dependencies between containers on the same Pod
- [Kubernetes PR #80744](https://github.com/kubernetes/kubernetes/pull/80744) - Sidecar Kubelet Implementation
- [Kubeternetes Sidecar Container KEP](https://github.com/kubernetes/enhancements/blob/master/keps/sig-apps/sidecarcontainers.md) - Kubernetes Enhancement Proposal for sidecar containers
- [Istio Issue #11130](https://github.com/istio/istio/issues/11130) - App container unable to connect to network before sidecar is fully running

## Description

The health-checker application in itself is very simple.  It exposes its own health endpoint at `/self` for use in liveness/readiness probes for the healthchecker sidecar container, which begins responding as soon as the GO webserver is available.  It then exposes an endpoint at `/all` for iteratively running healthchecks against a list of endpoints.  The application is reactive (it does not poll by itself on a schedule), so it's designed to be called by Kubernetes liveness and readiness probes or similar.  All other requests will return `404 Not Found`.

The application requires three arguments:

- `port`: the port on which the application should listen.
- `timeout`: the timeout (in seconds) to wait before counting an endpoint as failed.
- `endpoints`: a list of endpoints to check in the aggregated health check (1..n).

An example command line execution:

```bash
./healthcheck 8081 3 http://localhost:15020/healthz/ready http://localhost:8080/healthz
```

Which would listen on port `8081`, using a timeout of `3` when calling the two specified endpoints.  When the `/all` endpoint is called, it will iterate over the two endpoints and aggregate the response:

- If any of the endpoints return a status code `< 200` or `>= 400`, the health check will fail, returning a `503 Service Unavailable` response.  
- Likewise, if all endpoints return a status code `>= 200` and `< 400`, the health check will succeed, returning a `200 OK` response.

## Kubernetes Deployment

Here's an example of using this healthchecker in a k8s deployment when using Istio:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 2
  template:
    metadata:
      labels:
        app: my-app
        version: 0.1.0
    spec:
      restartPolicy: Always # Or OnFailure
      containers:
        - name: my-app
          image: my-app
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: 8080 # The port your application is exposed at
              protocol: TCP
          livenessProbe: # Liveness should point to some path within 'my-app' that can be used to determine if the _app container_ is alive.
            httpGet:
              path: /health
              port: http
          readinessProbe: # Readiness is pointed towards the aggregated health checker and only return 'ready' when both the Istio envoy proxy and my-app are healthy
            httpGet:
              path: /all
              port: 8081
        - name: health-check
          image: myrepo/healthcheck:0.1.0
          imagePullPolicy: IfNotPresent
          command: ["/app/healthcheck"]
          args: ["8081", "5", "http://localhost:8080/health", "http://localhost:15020/healthz/ready"] # Listen on 8081, timeout 5, check the 'my-app' health endpoint as well as the Istio proxy endpoint
          ports:
            - name: http
              containerPort: 8081
              protocol: TCP
          livenessProbe: # Check the '/self' endpoint to determine if the healthcheck container is alive
            httpGet:
              path: /self
              port: 8081
          readinessProbe: # Check the '/self' endpoint to determine if the healthcheck container is ready to serve traffic
            httpGet:
              path: /self
              port: 8081
```

Note that we are using the `Always` (or `OnFailure`) restart policy.  It's important that your app exits with a proper exit code (e.g. 1) and `not` zero when it fails to e.g. connect to a database or start-up properly due to network issues.  This will allow k8s to restart only the `my-app` container.

For all "app" containers, we change the readiness probe to use the healthcheck aggregator's `/all` endpoint.

## A Note for Kubernetes Jobs in Istio

_Since this is what got me looking into this in the first place, I'm including this footnote in case anyone stumbles across here with the same searches I probably did._

Jobs are one of the things that don't play extremely well with Istio and its Envoy proxy today, which was the first thing that got me looking into this.  The two main issues are:

1. The job is usually starting up directly wanting network access, and this usually happens prior to the envoy proxy being ready, resulting in the job crashing, the envoy proxy still running, the pod not terminating and the job staying alive indefinitely.
2. When the job has completed successfully, the envoy proxy will remain running, causing the pod to never exit. (note that the fix for this is to make a POST request to `http://localhost:15020/quitquitquit`).

I started out with using a custom shellscript as the entrypoint for the application, steered by a `USE_ISTIO` environment variable (so that the image could be run outside of the kubernetes environment), which I then used instead of the above solution:

```sh
#!/bin/sh

if [ ! -z $USE_ISTIO ] && [ $USE_ISTIO = "true" ]; then
  echo "Using istio configuration"
  echo "Waiting for Envoy proxy to become ready..."
  SC=0
  COUNT=0
  until [ $SC -eq "200" ]; do
    COUNT=$(($COUNT+1))
    SC=`curl -m 1 -s -o /dev/null -w "%{http_code}" http://localhost:15020/healthz/ready`
    echo " - Attempt #$COUNT - Status Code: $SC"
    if [ $COUNT -ge 30 ]; then
      echo "Exceeded number of connection attempts, asking Envoy proxy to quit"
      curl -X POST http://localhost:15020/quitquitquit
      exit 1
    fi
    sleep 1;
  done

  echo "Istio Envoy proxy is ready, starting application..."

  /app/$DOTNET_PROJECT && curl -X POST http://localhost:15020/quitquitquit
else
  echo "Using normal configuration, starting application..."
  /app/$DOTNET_PROJECT
fi

echo "Finished"
```

This worked well, but there were a few things that bothered me:

1. I didn't like having to make my builds & containers so "istio-aware"
2. I didn't like the dependency on CURL (we want to run hardened images)
3. I didn't like the dependency on having a shell (a problem for distroless & scratch images)

So, what I ended up doing was just letting the pod crash, and using a pod restart policy of `OnFailure`, and implementing a generic "completion callback" in the application (so as to not call it "kill istio proxy callback").  Not perfect, but I can live with that.  I configure the completion callback as part of the application configuration file, which is mounted to the container from a secret.  You could also use environment variables.

Here's an example of a simple job manifest:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: db-migration-runner
spec:
  template:
    metadata:
      name: db-migration-runner
      labels:
        app: db-migration-runner
        version: 0.1.0
    spec:
      restartPolicy: OnFailure
      containers:
      - name: db-migration-runner
        image: myrepo/db-migration-runner:0.1.0
        env:
        - name: ENABLE_COMPLETION_CALLBACK
          value: "1"
        - name: COMPLETION_CALLBACK_ENDPOINT
          value: "http://localhost:15020/quitquitquit"
```

**Note**: Make sure your container returns a non-zero exit code so Kubernetes knows its failed.  If you're entrypoint is a shellscript, the exit code of the _last command in the script_ will be returned, which in the case of the script above is `0`.  I fumbled on this, and it took me a while to realize that's why Kubernetes wasn't restarting my container.

## Credits

Credits to all those who have discussed these issues in related issues, and the code was closely derived/adapted from these repos/posts:

- https://github.com/Soluto/golang-docker-healthcheck-example
- https://medium.com/google-cloud/dockerfile-go-healthchecks-k8s-9a87d5c5b4cb