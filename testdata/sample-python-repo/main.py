from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List, Optional
import redis
from sqlalchemy import create_engine, Column, Integer, String
from sqlalchemy.ext.declarative import declarative_base
from sqlalchemy.orm import sessionmaker
import os
import json

# Initialize FastAPI app
app = FastAPI(title="Sample FastAPI App", version="1.0.0")

# Database setup
DATABASE_URL = os.getenv("DATABASE_URL", "postgresql://localhost/sampledb")
engine = create_engine(DATABASE_URL)
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)
Base = declarative_base()

# Redis setup
REDIS_URL = os.getenv("REDIS_URL", "redis://localhost:6379")
redis_client = redis.from_url(REDIS_URL, decode_responses=True)


# Models
class User(Base):
    __tablename__ = "users"

    id = Column(Integer, primary_key=True, index=True)
    name = Column(String, index=True)
    email = Column(String, unique=True, index=True)


# Pydantic schemas
class UserCreate(BaseModel):
    name: str
    email: str


class UserResponse(BaseModel):
    id: int
    name: str
    email: str

    class Config:
        from_attributes = True


# Health check endpoint
@app.get("/health")
async def health_check():
    return {
        "status": "healthy",
        "service": "sample-fastapi-app",
    }


# API endpoints
@app.get("/api/users", response_model=List[UserResponse])
async def get_users():
    """Get all users with Redis caching"""
    # Check cache first
    cached = redis_client.get("users:all")
    if cached:
        return json.loads(cached)

    # Query database
    db = SessionLocal()
    try:
        users = db.query(User).all()
        users_data = [
            {"id": u.id, "name": u.name, "email": u.email} for u in users
        ]

        # Cache result for 5 minutes
        redis_client.setex("users:all", 300, json.dumps(users_data))

        return users_data
    finally:
        db.close()


@app.post("/api/users", response_model=UserResponse, status_code=201)
async def create_user(user: UserCreate):
    """Create a new user"""
    db = SessionLocal()
    try:
        db_user = User(name=user.name, email=user.email)
        db.add(db_user)
        db.commit()
        db.refresh(db_user)

        # Invalidate cache
        redis_client.delete("users:all")

        return db_user
    finally:
        db.close()


@app.get("/api/users/{user_id}", response_model=UserResponse)
async def get_user(user_id: int):
    """Get a specific user by ID"""
    db = SessionLocal()
    try:
        user = db.query(User).filter(User.id == user_id).first()
        if user is None:
            raise HTTPException(status_code=404, detail="User not found")
        return user
    finally:
        db.close()


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)