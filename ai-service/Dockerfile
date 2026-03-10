FROM python:3.12-slim

# LibreOffice headless for PPTX → PDF conversion
RUN apt-get update && \
    apt-get install -y --no-install-recommends libreoffice-core libreoffice-impress \
    libcairo2 libcairo2-dev libffi-dev && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8001

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8001"]
