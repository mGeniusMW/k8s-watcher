from fastapi import FastAPI
from pydantic import BaseModel
from typing import List
import uvicorn

app = FastAPI()

# This must match your Go 'LocalPod' struct exactly
class LocalPod(BaseModel):
    name: str
    namespace: str
    status: str
    containerStatuses: List[str]

@app.post("/alerts")
async def receive_alert(pod: LocalPod):
    print(f"--- ALERT RECEIVED ---")
    print(f"Pod: {pod.namespace}/{pod.name}")
    print(f"Status: {pod.status}")
    print(f"Container States: {pod.containerStatuses}")
    return {"message": "Alert processed"}

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000)