"""ML tahmin servisi. Lot izlenebilirligi ve geri cagirma ML'e DAYANMAZ; ML sadece tahmindir.

Tum sayisal alanlar Decimal string'dir.
"""
from __future__ import annotations

from contextlib import asynccontextmanager
from datetime import date
from decimal import Decimal

from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field

from oracle.decimal_utils import dstr, qty3, ratio4

from .models.base import CUTTING, PRODUCTION
from .service import ModelService, frame


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.service = ModelService().bootstrap()
    yield


app = FastAPI(title="ML Tahmin Servisi", version="0.1.0", lifespan=lifespan)


def _service(request: Request) -> ModelService:
    service = getattr(request.app.state, "service", None)
    if service is None or not service.ready:
        raise HTTPException(status_code=503, detail="modeller yuklenmedi")
    return service


class ProductionRequest(BaseModel):
    et_kg: Decimal = Field(gt=0, le=100000, max_digits=12, decimal_places=3)
    supplier_id: str | None = None
    producer_id: str | None = None
    unit_weight_kg: Decimal = Field(default=Decimal("1.000"), gt=0, le=100, max_digits=8, decimal_places=3)
    order_date: date | None = None  # baseline kullanmaz; ileri modeller icin kabul edilir


class ProductionResponse(BaseModel):
    model_name: str
    model_version: str
    baseline: bool
    supplier_used: str
    fallback: bool
    et_kg: str
    unit_weight_kg: str
    predicted_yield_ratio: str
    predicted_output_kg: str
    predicted_delivered_qty: str  # adet = cikti kg / unit_weight_kg


class CuttingRequest(BaseModel):
    carcass_kg: Decimal = Field(gt=0, le=200, max_digits=9, decimal_places=3)
    breed: str | None = None
    sex: str | None = None


class CuttingPartOut(BaseModel):
    part: str
    kg: str
    ratio: str


class CuttingResponse(BaseModel):
    model_name: str
    model_version: str
    baseline: bool
    breed_used: str
    fallback: bool
    carcass_kg: str
    sellable_kg: str
    expected_fire_kg: str
    expected_fire_ratio: str
    parts: list[CuttingPartOut]


@app.get("/health")
def health(request: Request) -> dict[str, object]:
    service = getattr(request.app.state, "service", None)
    return {"status": "ok", "models_loaded": bool(service and service.ready)}


@app.get("/models")
def models(request: Request) -> dict[str, object]:
    return {"models": _service(request).describe()}


@app.post("/predict/production", response_model=ProductionResponse)
def predict_production(req: ProductionRequest, request: Request) -> ProductionResponse:
    loaded = _service(request).active(PRODUCTION)
    pred = loaded.model.predict(
        frame(
            et_kg=dstr(req.et_kg),
            supplier_id=req.supplier_id,
            producer_id=req.producer_id,
            unit_weight_kg=dstr(req.unit_weight_kg),
            order_date=req.order_date,
        )
    )[0]
    return ProductionResponse(
        model_name=loaded.info.name,
        model_version=loaded.info.version,
        baseline=loaded.info.baseline,
        supplier_used=pred.notes.get("supplier_used", "ALL"),
        fallback=bool(pred.notes.get("fallback", False)),
        et_kg=dstr(req.et_kg),
        unit_weight_kg=dstr(req.unit_weight_kg),
        predicted_yield_ratio=dstr(pred.yield_ratio),
        predicted_output_kg=dstr(pred.delivered_qty * req.unit_weight_kg),
        predicted_delivered_qty=dstr(pred.delivered_qty),
    )


@app.post("/predict/cutting", response_model=CuttingResponse)
def predict_cutting(req: CuttingRequest, request: Request) -> CuttingResponse:
    loaded = _service(request).active(CUTTING)
    pred = loaded.model.predict(frame(carcass_kg=dstr(req.carcass_kg), breed=req.breed, sex=req.sex))[0]
    carcass = qty3(req.carcass_kg)
    sellable = sum(pred.parts_kg.values(), Decimal(0))
    return CuttingResponse(
        model_name=loaded.info.name,
        model_version=loaded.info.version,
        baseline=loaded.info.baseline,
        breed_used=pred.notes.get("breed_used", "ALL"),
        fallback=bool(pred.notes.get("fallback", False)),
        carcass_kg=dstr(carcass),
        sellable_kg=dstr(sellable),
        expected_fire_kg=dstr(pred.fire_kg),
        expected_fire_ratio=dstr(pred.notes.get("expected_fire_ratio", ratio4(pred.fire_kg / carcass))),
        parts=[CuttingPartOut(part=p, kg=dstr(kg), ratio=dstr(ratio4(kg / carcass))) for p, kg in pred.parts_kg.items()],
    )
