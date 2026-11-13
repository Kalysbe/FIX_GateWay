"use strict";
window.addEventListener("DOMContentLoaded", function () {
  SwaggerUIBundle({
    url: "/api/broker-openapi.yaml",
    dom_id: "#swagger-ui",
    deepLinking: true,
    defaultModelsExpandDepth: 1,
    validatorUrl: null,
    // The broker token survives page reloads (browser localStorage) until Logout.
    persistAuthorization: true
  });
});
