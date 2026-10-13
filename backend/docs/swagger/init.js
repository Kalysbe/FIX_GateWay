"use strict";
window.addEventListener("DOMContentLoaded", function () {
  const broker = window.location.pathname === "/broker-swagger";
  SwaggerUIBundle({
    url: broker ? "/api/broker-openapi.yaml" : "/api/openapi.yaml",
    dom_id: "#swagger-ui",
    deepLinking: true,
    defaultModelsExpandDepth: 1,
    validatorUrl: null
  });
});
