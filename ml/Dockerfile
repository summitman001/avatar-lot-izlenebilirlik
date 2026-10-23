FROM python:3.11-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY oracle ./oracle
COPY data ./data
COPY app ./app
COPY train ./train
COPY reports ./reports

# Sentetik veri sabit seed ile imaj icinde uretilir (bkz. docs/ml-data.md)
RUN python -m data.generate_production && python -m data.generate_cutting

EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8000/health')"

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
