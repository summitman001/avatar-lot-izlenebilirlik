"""ML tahmin servisi. Lot izlenebilirligi ve geri cagirma ML'e DAYANMAZ; ML sadece tahmindir.

Tum sayisal alanlar Decimal string'dir.
"""
from __future__ import annotations

from contextlib import asynccontextmanager
from datetime import date
from decimal import Decimal

from fastapi import FastAPI, File, HTTPException, Request, UploadFile
from pydantic import BaseModel, Field

from oracle.decimal_utils import dstr, qty3, ratio4

from . import anomaly
from . import ocr as ocr_module
from .config import OCR, THRESHOLDS
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
    order_date: date | None = None  # baseline kullanmaz; ay turunden Ridge/GBM icin kullanilir
    # Asagidakiler backend sozlesmesinde ZORUNLU DEGIL (bkz. docs/assumptions.md madde 11-12);
    # verilmezse Ridge/GBM egitim ortalamalarini kullanir, baseline etkilenmez.
    lot_age_days: int | None = Field(default=None, ge=0, le=365)
    spice_kg: Decimal | None = Field(default=None, gt=0, le=1000, max_digits=10, decimal_places=3)
    casing_kg: Decimal | None = Field(default=None, gt=0, le=1000, max_digits=10, decimal_places=3)


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
            month=req.order_date.month if req.order_date else None,
            lot_age_days=req.lot_age_days,
            spice_kg=dstr(req.spice_kg) if req.spice_kg is not None else None,
            casing_kg=dstr(req.casing_kg) if req.casing_kg is not None else None,
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
        predicted_output_kg=dstr(qty3(pred.delivered_qty * req.unit_weight_kg)),
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


class AnomalyRequest(BaseModel):
    """Her alan opsiyoneldir: hangi alanlar verilirse yalnizca o kurallar calisir (best-effort).

    Sozlesme ve ornekler: docs/anomaly-integration.md.
    """

    reference_id: str | None = None

    # VERIM_DUSUK (production)
    actual_yield_ratio: Decimal | None = None
    et_kg: Decimal | None = Field(default=None, gt=0)
    supplier_id: str | None = None
    producer_id: str | None = None

    # FIRE_YUKSEK (cutting)
    actual_fire_kg: Decimal | None = Field(default=None, ge=0)
    carcass_kg: Decimal | None = Field(default=None, gt=0)
    breed: str | None = None
    sex: str | None = None

    # MALIYET_ARTISI
    current_unit_cost: Decimal | None = Field(default=None, gt=0)
    recent_unit_costs: list[Decimal] | None = None

    # EKP_SAPMA
    predicted_ekp_unit: Decimal | None = Field(default=None, gt=0)
    actual_ekp_unit: Decimal | None = Field(default=None, gt=0)

    # KUTLE_DENGESI
    input_weight_kg: Decimal | None = Field(default=None, gt=0)
    output_weights_kg: list[Decimal] | None = None


class AnomalyFindingOut(BaseModel):
    code: str
    severity: str
    message: str
    evidence: dict[str, str]


class AnomalyResponse(BaseModel):
    reference_id: str | None
    findings: list[AnomalyFindingOut]


def _residual_std(service: ModelService, task: str, field: str) -> float:
    cv = service.active(task).metrics.get("cv") or {}
    return float(cv.get(field, 0.0) or 0.0)


@app.post("/detect/anomaly", response_model=AnomalyResponse)
def detect_anomaly(req: AnomalyRequest, request: Request) -> AnomalyResponse:
    service = _service(request)
    findings: list[anomaly.AnomalyFinding] = []

    if req.actual_yield_ratio is not None and req.et_kg is not None:
        loaded = service.active(PRODUCTION)
        pred = loaded.model.predict(
            frame(et_kg=dstr(req.et_kg), supplier_id=req.supplier_id, producer_id=req.producer_id)
        )[0]
        sigma = _residual_std(service, PRODUCTION, "residual_std_yield_ratio")
        finding = anomaly.check_verim_dusuk(req.actual_yield_ratio, pred.yield_ratio, sigma, THRESHOLDS.yield_sigma)
        if finding:
            findings.append(finding)

    if req.actual_fire_kg is not None and req.carcass_kg is not None:
        loaded = service.active(CUTTING)
        pred = loaded.model.predict(frame(carcass_kg=dstr(req.carcass_kg), breed=req.breed, sex=req.sex))[0]
        actual_fire_ratio = ratio4(req.actual_fire_kg / req.carcass_kg)
        predicted_fire_ratio = pred.notes.get("expected_fire_ratio", ratio4(pred.fire_kg / req.carcass_kg))
        sigma = _residual_std(service, CUTTING, "residual_std_fire_ratio")
        finding = anomaly.check_fire_yuksek(actual_fire_ratio, predicted_fire_ratio, sigma, THRESHOLDS.fire_sigma)
        if finding:
            findings.append(finding)

    if req.current_unit_cost is not None and req.recent_unit_costs:
        finding = anomaly.check_maliyet_artisi(req.current_unit_cost, req.recent_unit_costs, THRESHOLDS.cost_increase_pct)
        if finding:
            findings.append(finding)

    if req.predicted_ekp_unit is not None and req.actual_ekp_unit is not None:
        finding = anomaly.check_ekp_sapma(req.actual_ekp_unit, req.predicted_ekp_unit, THRESHOLDS.ekp_deviation_pct)
        if finding:
            findings.append(finding)

    if req.input_weight_kg is not None and req.output_weights_kg is not None:
        finding = anomaly.check_kutle_dengesi(req.input_weight_kg, req.output_weights_kg)
        if finding:
            findings.append(finding)

    return AnomalyResponse(reference_id=req.reference_id, findings=[AnomalyFindingOut(**f.to_json()) for f in findings])


# ---------------------------------------------------------------------------
# Fatura/irsaliye OCR (bkz. docs/invoice-ocr.md). Model yuklenmesine (ModelService) DAYANMAZ:
# ML modelleri kapali/yuklenmemis olsa da (health degraded) bu uc nokta calisir.
# ---------------------------------------------------------------------------


class OcrFieldOut(BaseModel):
    value: str | None
    confidence: float
    source_text: str
    warnings: list[str]


class OcrLineOut(BaseModel):
    product_name: OcrFieldOut
    quantity: OcrFieldOut
    unit: OcrFieldOut
    unit_price: OcrFieldOut
    line_total: OcrFieldOut
    lot_no: OcrFieldOut
    expiry_date: OcrFieldOut
    warnings: list[str]


class InvoiceExtractionResponse(BaseModel):
    supplier_name: OcrFieldOut
    document_no: OcrFieldOut
    document_date: OcrFieldOut
    currency: OcrFieldOut
    lines: list[OcrLineOut]
    overall_confidence: float
    raw_text: str
    warnings: list[str]


def _field_out(f: ocr_module.FieldResult) -> OcrFieldOut:
    return OcrFieldOut(value=f.value, confidence=f.confidence, source_text=f.source_text, warnings=f.warnings)


def _line_out(l: ocr_module.LineResult) -> OcrLineOut:
    return OcrLineOut(
        product_name=_field_out(l.product_name), quantity=_field_out(l.quantity), unit=_field_out(l.unit),
        unit_price=_field_out(l.unit_price), line_total=_field_out(l.line_total),
        lot_no=_field_out(l.lot_no), expiry_date=_field_out(l.expiry_date), warnings=l.warnings,
    )


@app.post("/extract/invoice", response_model=InvoiceExtractionResponse)
async def extract_invoice(file: UploadFile = File(...)) -> InvoiceExtractionResponse:
    content = await file.read()
    if len(content) > OCR.max_file_size_bytes:
        raise HTTPException(status_code=413, detail="dosya boyutu siniri asildi")
    result = ocr_module.extract_invoice(content, file.content_type or "", file.filename or "")
    return InvoiceExtractionResponse(
        supplier_name=_field_out(result.supplier_name),
        document_no=_field_out(result.document_no),
        document_date=_field_out(result.document_date),
        currency=_field_out(result.currency),
        lines=[_line_out(l) for l in result.lines],
        overall_confidence=result.overall_confidence,
        raw_text=result.raw_text,
        warnings=result.warnings,
    )
