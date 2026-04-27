import logging
import time
from pathlib import Path

from joycode_proxy.credential_router import CredentialRouter
from joycode_proxy.openai_handler import create_openai_router
from joycode_proxy.anthropic_handler import create_anthropic_router

log = logging.getLogger("joycode-proxy")


def create_app(router: CredentialRouter, db=None):
    from fastapi import FastAPI, Request
    from fastapi.middleware.cors import CORSMiddleware
    from fastapi.staticfiles import StaticFiles

    app = FastAPI(title="JoyCode Proxy")
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )

    if db:
        @app.middleware("http")
        async def log_requests(request: Request, call_next):
            start = time.time()
            response = await call_next(request)
            latency = int((time.time() - start) * 1000)
            path = request.url.path
            if path.startswith("/v1/") or path.startswith("/api/"):
                api_key = request.headers.get("x-api-key", "")
                model = ""
                if request.method == "POST" and path.startswith("/v1/"):
                    try:
                        import json
                        body_bytes = await request.body()
                        if body_bytes:
                            body = json.loads(body_bytes)
                            model = body.get("model", "")
                    except Exception:
                        pass
                db.log_request(
                    api_key=api_key, model=model, endpoint=path,
                    stream=False, status_code=response.status_code,
                    latency_ms=latency,
                )
            return response

        from joycode_proxy.web_api import create_web_api_router
        app.include_router(create_web_api_router(db))

    app.include_router(create_openai_router(router))
    app.include_router(create_anthropic_router(router))

    static_dir = Path(__file__).parent / "static"
    if static_dir.is_dir():
        app.mount("/", StaticFiles(directory=str(static_dir), html=True), name="static")

    return app
