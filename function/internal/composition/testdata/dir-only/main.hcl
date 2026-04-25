resource cmap {
  body = {
    apiVersion = "v1"
    kind = "ConfigMap"
    data = { foo = "bar" }
  }
}
