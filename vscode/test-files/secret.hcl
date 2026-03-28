
resource my_secret {
  body = {
    apiVersion = "v1"
    kind = "Secret"
    metadata = {
      name = "my-secret"
      namespace = "default"
    }
    type = "Opaque"
    stringData = {
      username = "admin"
      password = "not-a-real-secret" # non-sensitive test data
    }
  }
}
