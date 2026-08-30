"""password reset codes

Revision ID: f3c8a1b2d4e6
Revises: a1f2c3d4e5b6
Create Date: 2026-08-30 00:00:00.000000

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = 'f3c8a1b2d4e6'
down_revision: Union[str, Sequence[str], None] = 'a1f2c3d4e5b6'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Upgrade schema."""
    with op.batch_alter_table('users', schema=None) as batch_op:
        batch_op.add_column(sa.Column('password_reset_code_hash', sa.String(length=64), nullable=True))
        batch_op.add_column(sa.Column('password_reset_code_expires_at', sa.DateTime(timezone=True), nullable=True))
        batch_op.add_column(sa.Column('password_reset_code_last_sent_at', sa.DateTime(timezone=True), nullable=True))


def downgrade() -> None:
    """Downgrade schema."""
    with op.batch_alter_table('users', schema=None) as batch_op:
        batch_op.drop_column('password_reset_code_last_sent_at')
        batch_op.drop_column('password_reset_code_expires_at')
        batch_op.drop_column('password_reset_code_hash')
