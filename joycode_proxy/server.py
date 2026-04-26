from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from joycode_proxy.client import Client
from joycode_proxy.openai_handler import create_openai_router
from joycode_proxy.anthropic_handler import create_anthropic_router


def create_app(client: Client) -> FastAPI:
    app = FastAPI(title="JoyCode Proxy")
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.include_router(create_openai_router(client))
    app.include_router(create_anthropic_router(client))
    return app
