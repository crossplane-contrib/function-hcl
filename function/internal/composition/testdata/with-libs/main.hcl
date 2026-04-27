locals {
  foo = invoke("bar", { input = 10 })
}

resource cmap {
  body = {
    apiVersion = "v1"
    kind = "ConfigMap"
    data = { foo = foo }
  }
}
