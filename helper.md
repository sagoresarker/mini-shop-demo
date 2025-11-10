loki log viewer
```sh
curl -G -s "http://localhost:3100/loki/api/v1/query" --data-urlencode 'query={service="product-service"}'| jq -r '.data.result[0].values[][1]' | jq .
```
look for trace-id
```sh
docker ps -q | xargs -I {} sh -c "echo '--- {} ---'; docker logs {} 2>&1 | grep 1f7bcaad3315db5c8c876935dd6a4aea"
```

```sh
curl "http://localhost:3100/loki/api/v1/label/service/values"
```